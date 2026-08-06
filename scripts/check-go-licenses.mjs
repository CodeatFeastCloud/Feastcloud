// SPDX-License-Identifier: AGPL-3.0-only

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';

const workspace = resolve(import.meta.dirname, '..');
const coreModule = resolve(workspace, 'services/core');
const edgeModule = resolve(workspace, 'services/edge');
const approved = new Map([
  ['github.com/dustin/go-humanize', 'MIT'],
  ['github.com/google/uuid', 'BSD-3-Clause'],
  ['github.com/jackc/pgpassfile', 'MIT'],
  ['github.com/jackc/pgservicefile', 'MIT'],
  ['github.com/jackc/pgx/v5', 'MIT'],
  ['github.com/jackc/puddle/v2', 'MIT'],
  ['github.com/mattn/go-isatty', 'MIT'],
  ['github.com/ncruces/go-strftime', 'MIT'],
  ['github.com/remyoudompheng/bigfft', 'BSD-3-Clause'],
  ['golang.org/x/exp', 'BSD-3-Clause'],
  ['golang.org/x/crypto', 'BSD-3-Clause'],
  ['golang.org/x/sync', 'BSD-3-Clause'],
  ['golang.org/x/sys', 'BSD-3-Clause'],
  ['golang.org/x/text', 'BSD-3-Clause'],
  ['modernc.org/libc', 'BSD-3-Clause'],
  ['modernc.org/mathutil', 'BSD-3-Clause'],
  ['modernc.org/memory', 'BSD-3-Clause'],
  ['modernc.org/sqlite', 'BSD-3-Clause'],
]);

const format = '{{with .Module}}{{if not .Main}}{{.Dir}}|{{.Path}}|{{.Version}}{{end}}{{end}}';
const output = [coreModule, edgeModule]
  .map((module) =>
    execFileSync('go', ['list', '-deps', '-f', format, './...'], {
      cwd: module,
      encoding: 'utf8',
      env: process.env,
    }),
  )
  .join('\n');

const dependencies = [...new Set(output.split(/\r?\n/).filter(Boolean))]
  .map((line) => {
    const [directory, modulePath, version] = line.split('|');
    return { directory, modulePath, version };
  })
  .sort((left, right) => left.modulePath.localeCompare(right.modulePath));

const failures = [];
for (const dependency of dependencies) {
  const expected = approved.get(dependency.modulePath);
  if (!expected) {
    failures.push(`${dependency.modulePath}@${dependency.version}: module has not received license review`);
    continue;
  }
  if (!existsSync(dependency.directory)) {
    failures.push(`${dependency.modulePath}@${dependency.version}: module source directory is unavailable`);
    continue;
  }
  const licenseFiles = readdirSync(dependency.directory)
    .filter((name) => /^licen[cs]e(?:[-._].*)?$/i.test(name))
    .sort();
  if (licenseFiles.length === 0) {
    failures.push(`${dependency.modulePath}@${dependency.version}: no top-level license file`);
    continue;
  }
  for (const name of licenseFiles) {
    const text = readFileSync(resolve(dependency.directory, name), 'utf8');
    const recognized =
      text.includes('Permission is hereby granted, free of charge') ||
      text.includes('Redistribution and use in source and binary forms');
    if (!recognized) {
      failures.push(`${dependency.modulePath}@${dependency.version}: ${name} is not recognized as MIT/BSD`);
    }
  }
  console.log(`${dependency.modulePath}@${dependency.version} — ${expected}`);
}

if (failures.length > 0) {
  console.error('\nGo dependency license review failed:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exitCode = 1;
} else {
  console.log(`\nAll ${dependencies.length} compiled Go modules have approved OSI licenses.`);
}
