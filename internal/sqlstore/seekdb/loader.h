#pragma once

#include <stddef.h>

typedef struct PCSeekDBLibrary PCSeekDBLibrary;

typedef struct {
    const char *transport;
    unsigned int port;
    const char *endpoint;
    const char *user;
} PCSeekDBConnectionOptions;

int pc_seekdb_library_open(const char *path, PCSeekDBLibrary **out_library, char **out_error);
void pc_seekdb_library_close(PCSeekDBLibrary *library);
int pc_seekdb_instance_open(
    PCSeekDBLibrary *library,
    const char *directory,
    void **out_handle
);
int pc_seekdb_connection_options(
    PCSeekDBLibrary *library,
    void *handle,
    PCSeekDBConnectionOptions *out_options
);
int pc_seekdb_instance_close(PCSeekDBLibrary *library, void *handle);
void pc_seekdb_error_free(char *message);
