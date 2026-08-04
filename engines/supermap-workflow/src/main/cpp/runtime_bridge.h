#pragma once

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct AddpSuperMapRuntime AddpSuperMapRuntime;

typedef struct AddpRuntimeResponse {
  int status;
  char* body;
  size_t body_size;
} AddpRuntimeResponse;

AddpSuperMapRuntime* addp_supermap_runtime_create(
    const char* operators_config,
    const char* sdk_root,
    char** error_message);

void addp_supermap_runtime_destroy(AddpSuperMapRuntime* runtime);
void addp_supermap_runtime_free_string(char* value);
void addp_supermap_runtime_free_response(AddpRuntimeResponse response);

AddpRuntimeResponse addp_supermap_runtime_health(AddpSuperMapRuntime* runtime);
AddpRuntimeResponse addp_supermap_runtime_operators(
    AddpSuperMapRuntime* runtime,
    const char* category);
AddpRuntimeResponse addp_supermap_runtime_workflow(
    AddpSuperMapRuntime* runtime,
    const char* body,
    size_t body_size);
AddpRuntimeResponse addp_supermap_runtime_invoke(
    AddpSuperMapRuntime* runtime,
    const char* operator_name,
    const char* body,
    size_t body_size);
AddpRuntimeResponse addp_supermap_runtime_execution(
    AddpSuperMapRuntime* runtime,
    const char* execution_id);

#ifdef __cplusplus
}
#endif
