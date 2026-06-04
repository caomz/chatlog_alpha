#!/usr/bin/env node
import { access, readFile, stat } from 'node:fs/promises';
import path from 'node:path';

const root = process.cwd();
const checks = [];

await fileExists('AGENTS.md');
await fileExists('CLAUDE.md');
await fileExists('feature_list.json');
await fileExists('progress.md');
await fileExists('session-handoff.md');
await fileExists('init.sh');

const agents = await readText('AGENTS.md');
const claude = await readText('CLAUDE.md');
const progress = await readText('progress.md');
const handoff = await readText('session-handoff.md');
const init = await readText('init.sh');
const featureListText = await readText('feature_list.json');

checkIncludes(agents, 'Startup Workflow', 'AGENTS.md documents startup workflow');
checkIncludes(agents, 'Definition of Done', 'AGENTS.md documents Definition of Done');
checkIncludes(agents, 'Verification Commands', 'AGENTS.md documents verification commands');
checkIncludes(agents, 'feature_list.json', 'AGENTS.md routes state artifacts');
checkIncludes(agents, 'One feature at a time', 'AGENTS.md enforces one active feature');
checkIncludes(agents, 'Stay in scope', 'AGENTS.md documents scope boundary');
checkIncludes(agents, 'End of Session', 'AGENTS.md documents end-of-session routine');
checkIncludes(claude, '@AGENTS.md', 'CLAUDE.md imports AGENTS.md');
checkIncludes(progress, 'Verification Evidence', 'progress.md records verification evidence');
checkIncludes(progress, 'Recommended Next Step', 'progress.md has restart marker');
checkIncludes(handoff, 'Next Session', 'session-handoff.md has next session section');
checkIncludes(handoff, 'Blockers', 'session-handoff.md records blockers');
checkIncludes(handoff, 'Files', 'session-handoff.md records files');
checkIncludes(init, 'set -e', 'init.sh fails fast');
checkIncludes(init, 'go test ./...', 'init.sh documents/runs test command');
checkIncludes(init, 'make build', 'init.sh documents/runs build command');
checkIncludes(init, 'check-harness-skill.mjs', 'init.sh runs repo-local skill check');
checkIncludes(init, 'check-root-harness.mjs', 'init.sh runs root harness check');
checkNotIncludesDefaultRun(init, '--vision', 'init.sh does not run --vision as a command');
checkNotIncludesDefaultRun(init, '--summary', 'init.sh does not run --summary as a command');
await executable('init.sh');
checkFeatureList(featureListText);

const failed = checks.filter((check) => !check.pass);
for (const check of checks) {
  console.log(`${check.pass ? 'PASS' : 'FAIL'} ${check.message}`);
}
console.log('');
console.log(`Root harness check: ${checks.length - failed.length}/${checks.length} passed`);

if (failed.length > 0) {
  process.exitCode = 1;
}

async function fileExists(relativePath) {
  try {
    await access(path.join(root, relativePath));
    checks.push(pass(`file exists: ${relativePath}`));
  } catch {
    checks.push(fail(`file exists: ${relativePath}`));
  }
}

async function readText(relativePath) {
  try {
    return await readFile(path.join(root, relativePath), 'utf8');
  } catch {
    return '';
  }
}

async function executable(relativePath) {
  try {
    const mode = (await stat(path.join(root, relativePath))).mode;
    checks.push((mode & 0o111) ? pass(`${relativePath} is executable`) : fail(`${relativePath} is executable`));
  } catch {
    checks.push(fail(`${relativePath} is executable`));
  }
}

function checkIncludes(text, needle, message) {
  checks.push(text.includes(needle) ? pass(message) : fail(`${message} (${needle})`));
}

function checkNotIncludesDefaultRun(text, needle, message) {
  const commandLines = text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.startsWith('run ') || line.startsWith('chatlog '));
  checks.push(commandLines.some((line) => line.includes(needle)) ? fail(message) : pass(message));
}

function checkFeatureList(text) {
  let parsed;
  try {
    parsed = JSON.parse(text);
    checks.push(pass('feature_list.json parses as JSON'));
  } catch (error) {
    checks.push(fail(`feature_list.json parses as JSON (${error.message})`));
    return;
  }

  checks.push(typeof parsed.active_feature_id === 'string' && parsed.active_feature_id.length > 0
    ? pass('feature_list.json has active_feature_id')
    : fail('feature_list.json has active_feature_id'));

  const features = Array.isArray(parsed.features) ? parsed.features : [];
  checks.push(features.length > 0 ? pass('feature_list.json has features') : fail('feature_list.json has features'));

  const active = features.find((feature) => feature.id === parsed.active_feature_id);
  checks.push(active ? pass('active_feature_id matches a feature') : fail('active_feature_id matches a feature'));

  const requiredFields = [
    'id',
    'name',
    'title',
    'description',
    'status',
    'scope',
    'done_criteria',
    'evidence',
    'next_step',
    'dependencies'
  ];
  for (const field of requiredFields) {
    checks.push(features.every((feature) => Object.hasOwn(feature, field))
      ? pass(`all features include ${field}`)
      : fail(`all features include ${field}`));
  }
}

function pass(message) {
  return { pass: true, message };
}

function fail(message) {
  return { pass: false, message };
}

