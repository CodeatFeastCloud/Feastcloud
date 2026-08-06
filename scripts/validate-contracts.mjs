// SPDX-License-Identifier: AGPL-3.0-only
import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";

const schemaDirectory = new URL("../packages/contracts/schemas/", import.meta.url);
const exampleDirectory = new URL("../packages/contracts/examples/", import.meta.url);
const entries = (await readdir(schemaDirectory)).filter((entry) => entry.endsWith(".json"));
const schemas = new Map();

if (entries.length === 0) {
  throw new Error("No public contract schemas were found");
}

for (const entry of entries) {
  const source = await readFile(join(schemaDirectory.pathname, entry), "utf8");
  const schema = JSON.parse(source);
  if (schema.$schema !== "https://json-schema.org/draft/2020-12/schema") {
    throw new Error(`${entry} must use JSON Schema draft 2020-12`);
  }
  if (!schema.$id || !schema.title || schema.additionalProperties !== false) {
    throw new Error(`${entry} must define $id, title, and additionalProperties=false`);
  }
  schemas.set(entry, schema);
}

const validator = new Ajv2020({ allErrors: true, strict: true });
addFormats(validator);
for (const schema of schemas.values()) validator.addSchema(schema);

const examples = (await readdir(exampleDirectory)).filter((entry) => entry.endsWith(".json"));
const exampleSchemas = new Map([
  ["connector.csv.json", "connector-manifest.json"],
  ["country.in.json", "country-pack.json"],
  ["hardware.escpos.json", "hardware-adapter.json"],
  ["language.bn.json", "language-pack.json"],
  ["language.en.json", "language-pack.json"],
  ["language.hi.json", "language-pack.json"],
  ["language-pack-index.empty.json", "language-pack-index.json"],
  ["workflow.cloud-kitchen.json", "workflow-pack.json"],
]);
for (const entry of examples) {
  const example = JSON.parse(await readFile(join(exampleDirectory.pathname, entry), "utf8"));
  const schemaName = exampleSchemas.get(entry);
  if (!schemaName) throw new Error(`${entry} has no declared schema mapping`);
  const validate = validator.getSchema(schemas.get(schemaName).$id);
  if (!validate(example)) {
    throw new Error(`${entry} violates ${schemaName}: ${validator.errorsText(validate.errors)}`);
  }
}

const bundledLanguageIndex = JSON.parse(
  await readFile(new URL("../apps/web/public/language-packs/index.json", import.meta.url), "utf8"),
);
const validateLanguageIndex = validator.getSchema(schemas.get("language-pack-index.json").$id);
if (!validateLanguageIndex(bundledLanguageIndex)) {
  throw new Error(`bundled PWA language index is invalid: ${validator.errorsText(validateLanguageIndex.errors)}`);
}

const candidates = JSON.parse(
  await readFile(new URL("../models/candidates.json", import.meta.url), "utf8"),
);
if (candidates.schemaVersion !== "1.0" || !Array.isArray(candidates.models)) {
  throw new Error("models/candidates.json does not use the expected registry contract");
}
const validateModel = validator.getSchema(schemas.get("model-manifest.json").$id);
for (const model of candidates.models) {
  if (!validateModel(model)) {
    throw new Error(`model ${model.id ?? "<unknown>"} violates model-manifest.json: ${validator.errorsText(validateModel.errors)}`);
  }
}

const validateMutation = validator.getSchema(schemas.get("mutation-envelope.json").$id);
const mutationFixture = {
  id: "019cfeb0-0001-7000-8000-000000000001",
  tenantId: "tenant-contract-test",
  outletId: "outlet-contract-test",
  deviceId: "device-contract-test",
  actorId: "actor-contract-test",
  occurredAt: "2026-08-03T00:00:00Z",
  source: "contract-test",
  schemaVersion: "1.0",
  idempotencyKey: "contract-test-0001",
  payload: {},
};
if (!validateMutation(mutationFixture)) {
  throw new Error(`canonical mutation fixture is invalid: ${validator.errorsText(validateMutation.errors)}`);
}

const edgeOpenApi = JSON.parse(
  await readFile(new URL("../services/edge/api/openapi.json", import.meta.url), "utf8"),
);
if (edgeOpenApi.openapi !== "3.1.0" || !edgeOpenApi.paths?.["/api/v1/sync/mutations"]) {
  throw new Error("services/edge/api/openapi.json is not the expected OpenAPI 3.1 contract");
}

const coreOpenApi = await readFile(
  new URL("../services/core/api/openapi.yaml", import.meta.url),
  "utf8",
);
if (!coreOpenApi.includes("openapi: 3.1.0") || !coreOpenApi.includes("/api/v1/sync/operations:")) {
  throw new Error("services/core/api/openapi.yaml is not the expected OpenAPI 3.1 contract");
}

const syncProto = await readFile(
  new URL("../packages/contracts/proto/feastcloud/sync/v1/sync.proto", import.meta.url),
  "utf8",
);
if (!syncProto.includes('syntax = "proto3";') || !syncProto.includes("service EdgeSync")) {
  throw new Error("the edge synchronization Protobuf contract is incomplete");
}

console.log(
  `Validated ${entries.length} public schemas, ${examples.length} examples, ${candidates.models.length} model candidates, 2 OpenAPI contracts, and the edge Protobuf source.`,
);
