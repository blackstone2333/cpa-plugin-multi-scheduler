package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static int call_host_api(cliproxy_host_api* host, const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (host == NULL || host->call == NULL) {
		return 1;
	}
	return host->call(host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(cliproxy_host_api* host, void* ptr, size_t len) {
	if (host != NULL && host->free_buffer != NULL && ptr != NULL) {
		host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

var (
	hostAPI    atomic.Pointer[C.cliproxy_host_api]
	globalMu   sync.RWMutex
	stateStore = NewSafeStateStore()
	appConfig  = DefaultConfig()
	refresher  *QuotaRefresher
	mgmtSvc    *ManagementService
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	hostAPI.Store(host)

	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method required"))
		return 0
	}

	var reqBytes []byte
	if request != nil && requestLen > 0 {
		reqBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}

	methodStr := C.GoString(method)
	resBytes, err := dispatchMethod(methodStr, reqBytes)
	if err != nil {
		writeResponse(response, errorEnvelope("internal_error", err.Error()))
		return 0
	}
	writeResponse(response, resBytes)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if refresher != nil {
		refresher.Stop()
		refresher = nil
	}
	hostAPI.Store(nil)
}

func dispatchMethod(method string, req []byte) ([]byte, error) {
	defer func() {
		if r := recover(); r != nil {
			// Defend CPA host against unexpected panics
		}
	}()

	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return handleRegister(req)
	case pluginabi.MethodSchedulerPick:
		return handleSchedulerPick(req)
	case pluginabi.MethodUsageHandle:
		return handleUsage(req)
	case pluginabi.MethodManagementRegister:
		return handleManagementRegister(req)
	case pluginabi.MethodManagementHandle:
		return handleManagementHandle(req)
	default:
		return errorEnvelope("unknown_method", "method not implemented: "+method), nil
	}
}

func handleRegister(raw []byte) ([]byte, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}

	cfg, err := DecodeConfig(req.ConfigYAML)
	if err == nil {
		appConfig = cfg
	}

	if refresher != nil {
		refresher.Stop()
	}
	refresher = NewQuotaRefresher(appConfig, stateStore, callHost)
	if appConfig.Enabled {
		refresher.Start()
	}
	mgmtSvc = NewManagementService(stateStore, appConfig)

	type rpcCaps struct {
		Scheduler     bool `json:"scheduler"`
		ManagementAPI bool `json:"management_api"`
		UsagePlugin   bool `json:"usage_plugin"`
	}

	type regResponse struct {
		SchemaVersion uint32             `json:"schema_version"`
		Metadata      pluginapi.Metadata `json:"metadata"`
		Capabilities  rpcCaps            `json:"capabilities"`
	}

	res := regResponse{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:        "CPA账户动态编排调配",
			Version:     "0.1.0",
			Author:      "blackstone2333",
			GitHubRepository: "https://github.com/blackstone2333/cpa-plugin-multi-scheduler",
		},
		Capabilities: rpcCaps{
			Scheduler:     true,
			ManagementAPI: true,
			UsagePlugin:   true,
		},
	}
	return okEnvelope(res)
}

func handleSchedulerPick(raw []byte) ([]byte, error) {
	var req pluginapi.SchedulerPickRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
	}

	// If disabled in config, fallback to CPA default
	globalMu.RLock()
	enabled := appConfig.Enabled
	globalMu.RUnlock()
	if !enabled {
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}

	// Register incoming candidates if unknown
	for _, c := range req.Candidates {
		if _, found := stateStore.Get(c.ID); !found {
			stateStore.Put(&AccountState{
				Key:             c.ID,
				AuthID:          c.ID,
				Provider:        c.Provider,
				Name:            c.ID,
				Alias:           AccountAlias(c.ID),
				CurrentPriority: c.Priority,
			})
		}
	}

	selectedID, handled := SelectBestAccount(req.Candidates, req.Provider, stateStore)
	if !handled {
		return okEnvelope(pluginapi.SchedulerPickResponse{
			Handled:         true,
			DelegateBuiltin: pluginapi.SchedulerBuiltinRoundRobin,
		})
	}

	return okEnvelope(pluginapi.SchedulerPickResponse{
		AuthID:  selectedID,
		Handled: true,
	})
}

func handleUsage(raw []byte) ([]byte, error) {
	var record pluginapi.UsageRecord
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &record)
	}
	return okEnvelope(map[string]any{"ok": true})
}

func handleManagementRegister(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRegistrationRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	if mgmtSvc == nil {
		mgmtSvc = NewManagementService(stateStore, appConfig)
	}
	res, err := mgmtSvc.RegisterManagement(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return okEnvelope(res)
}

func handleManagementHandle(raw []byte) ([]byte, error) {
	var req pluginapi.ManagementRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	if mgmtSvc == nil {
		mgmtSvc = NewManagementService(stateStore, appConfig)
	}
	res, err := mgmtSvc.HandleManagement(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return okEnvelope(res)
}

func callHost(method string, payload any) (json.RawMessage, error) {
	h := hostAPI.Load()
	if h == nil || h.call == nil {
		return nil, fmt.Errorf("host api unavailable")
	}

	var reqBytes []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reqBytes = b
	}

	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var cReq unsafe.Pointer
	if len(reqBytes) > 0 {
		cReq = C.CBytes(reqBytes)
		defer C.free(cReq)
	}

	var resp C.cliproxy_buffer
	rc := C.call_host_api(h, cMethod, (*C.uint8_t)(cReq), C.size_t(len(reqBytes)), &resp)
	if rc != 0 {
		return nil, fmt.Errorf("host call %s failed with code %d", method, int(rc))
	}

	var out []byte
	if resp.ptr != nil && resp.len > 0 {
		out = C.GoBytes(resp.ptr, C.int(resp.len))
		C.free_host_buffer(h, resp.ptr, resp.len)
	}

	var env pluginabi.Envelope
	if len(out) > 0 {
		if err := json.Unmarshal(out, &env); err == nil && env.OK {
			return env.Result, nil
		}
	}
	return out, nil
}

func okEnvelope(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pluginabi.Envelope{
		OK:     true,
		Result: raw,
	})
}

func errorEnvelope(code, msg string) []byte {
	raw, _ := json.Marshal(pluginabi.Envelope{
		OK: false,
		Error: &pluginabi.Error{
			Code:    code,
			Message: msg,
		},
	})
	return raw
}

func writeResponse(buf *C.cliproxy_buffer, data []byte) {
	if buf == nil || len(data) == 0 {
		return
	}
	ptr := C.CBytes(data)
	if ptr == nil {
		return
	}
	buf.ptr = ptr
	buf.len = C.size_t(len(data))
}
