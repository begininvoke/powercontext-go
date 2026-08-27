/*
 * Copyright (c) 2026 OceanBase.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

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
