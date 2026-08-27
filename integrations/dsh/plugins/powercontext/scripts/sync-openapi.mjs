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

import { copyFileSync, existsSync, mkdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const dest = join(root, 'openapi', 'powercontext.yaml')

function existingFile(path) {
  return path && existsSync(path) ? resolve(path) : undefined
}

function walkForOpenApi(startDir, excludedPath) {
  let dir = resolve(startDir)
  for (let i = 0; i < 8; i += 1) {
    const candidate = join(dir, 'openapi', 'powercontext.yaml')
    const resolvedCandidate = resolve(candidate)
    if (existsSync(candidate) && resolvedCandidate !== excludedPath) return resolvedCandidate
    const parent = resolve(dir, '..')
    if (parent === dir) break
    dir = parent
  }
  return undefined
}

export function resolvePowerContextRoot() {
  const fromEnv = process.env.POWERCONTEXT_ROOT?.trim()
  if (fromEnv && existsSync(fromEnv)) return resolve(fromEnv)
  // In a source checkout, prefer the repository's authoritative OpenAPI over
  // this package's committed offline copy. Otherwise a stale embedded copy can
  // make generation and its drift tests agree with each other while both have
  // silently diverged from the Server contract.
  const yamlPath = walkForOpenApi(root, resolve(dest))
  if (!yamlPath) return undefined
  return resolve(dirname(yamlPath), '..')
}

export function resolveOpenApiPath() {
  const fromEnv = existingFile(process.env.POWERCONTEXT_OPENAPI?.trim())
  if (fromEnv) return fromEnv
  const checkout = resolvePowerContextRoot()
  const fromRoot = checkout
    ? existingFile(join(checkout, 'openapi', 'powercontext.yaml'))
    : undefined
  if (fromRoot) return fromRoot
  const walked = walkForOpenApi(root, resolve(dest))
  if (walked) return walked
  if (existsSync(dest)) return dest
  throw new Error(
    'openapi/powercontext.yaml is missing. Point POWERCONTEXT_ROOT or POWERCONTEXT_OPENAPI at a PowerContext checkout.',
  )
}

export function syncOpenApi() {
  const source = resolveOpenApiPath()
  if (source === dest) return dest
  mkdirSync(dirname(dest), { recursive: true })
  copyFileSync(source, dest)
  return dest
}

const invokedDirectly = process.argv[1] !== undefined
  && import.meta.url === pathToFileURL(process.argv[1]).href
if (invokedDirectly) {
  console.log(`openapi synced to ${syncOpenApi()}`)
}
