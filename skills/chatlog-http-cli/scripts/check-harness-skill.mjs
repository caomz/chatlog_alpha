#!/usr/bin/env node
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const skillRoot = path.resolve(scriptDir, '..');

const files = [
  'SKILL.md',
  'references/harness-map.md',
  'references/verification-gates.md',
  'references/session-state.md',
  'references/feedback-audit.md'
];

const checks = [];
const contents = new Map();

for (const file of files) {
  const fullPath = path.join(skillRoot, file);
  try {
    contents.set(file, await readFile(fullPath, 'utf8'));
    checks.push(pass(`file exists: ${file}`));
  } catch {
    checks.push(fail(`file exists: ${file}`));
  }
}

const allText = [...contents.values()].join('\n\n').toLowerCase();

checkContains('five subsystems: instructions', ['instructions', '指令']);
checkContains('five subsystems: tools', ['tools', '工具']);
checkContains('five subsystems: environment', ['environment', '环境']);
checkContains('five subsystems: state', ['state', '状态']);
checkContains('five subsystems: feedback', ['feedback', '反馈']);

checkContains('startup map mode', ['startup map']);
checkContains('verification gate mode', ['verification gate']);
checkContains('session continuity mode', ['session continuity']);
checkContains('anti-false-done mode', ['anti-false-done', '防']);

checkContains('repo as source of truth', ['repository as the only source of truth', '仓库']);
checkContains('cross-session continuity', ['3-minute restore', '跨会话', 'session']);
checkContains('no premature completion', ['do not claim completion', '未验证']);
checkContains('feedback subsystem high leverage', ['feedback is usually the cheapest', '反馈子系统']);
checkContains('harness debt', ['harness debt', 'harness decays']);
checkContains('control-variable method', ['control-variable', '控制变量']);

checkContains('go test gate', ['go test ./...']);
checkContains('make build gate', ['make build']);
checkContains('daily help gate', ['go run . report daily --help']);
checkContains('http health gate', ['/health', '/api/v1/ping']);
checkContains('quota warning', ['--vision', '--summary', 'quota']);

const failed = checks.filter((check) => !check.pass);

for (const check of checks) {
  console.log(`${check.pass ? 'PASS' : 'FAIL'} ${check.message}`);
}

console.log('');
console.log(`Harness skill check: ${checks.length - failed.length}/${checks.length} passed`);

if (failed.length > 0) {
  process.exitCode = 1;
}

function checkContains(message, needles) {
  checks.push(needles.some((needle) => allText.includes(String(needle).toLowerCase()))
    ? pass(message)
    : fail(`${message} (${needles.join(' | ')})`));
}

function pass(message) {
  return { pass: true, message };
}

function fail(message) {
  return { pass: false, message };
}
