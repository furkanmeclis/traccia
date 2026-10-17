#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

// Commit metadata must retain the human author's identity without tool signatures.
const assistantName = /\b(?:codex|openai|claude|chatgpt|copilot|gemini|aider|gpt(?:[- ]?\d[\w.-]*)?)\b/i;
const generatedSignature = /(?:generated|written|authored)\s+(?:with|by)\s+(?:an?\s+)?(?:ai|assistant|bot)\b/i;
const botIdentity = /(?:\[bot\]|\b(?:ai[- ]assistant|coding[- ]agent)\b)/i;

function check(label, message, identities = '') {
  if (assistantName.test(message) || generatedSignature.test(message) || botIdentity.test(message)
      || assistantName.test(identities) || botIdentity.test(identities)) {
    console.error(`${label}: assistant attribution is not allowed in commit metadata.`);
    return false;
  }
  return true;
}

const args = process.argv.slice(2);
let valid = true;
if (args.length === 2 && args[0] === '--file') {
  valid = check('Commit message', readFileSync(args[1], 'utf8'));
} else if (args.length === 0 || (args.length === 1 && args[0] === '--all')) {
  const revisions = args[0] === '--all' ? ['--all'] : ['HEAD'];
  const history = execFileSync('git', [
    'log', ...revisions, '--format=%H%x00%an <%ae>%n%cn <%ce>%x00%B%x00%x00',
  ], { encoding: 'utf8' });
  let count = 0;
  for (const record of history.split('\0\0')) {
    if (!record.trim()) continue;
    const [sha, identities, message] = record.trimStart().split('\0');
    valid = check(sha, message, identities) && valid;
    count += 1;
  }
  if (valid) console.log(`Commit attribution check passed (${count} commits).`);
} else {
  console.error('Usage: node scripts/check-commit-message.mjs [--all | --file <message-file>]');
  process.exit(2);
}
process.exitCode = valid ? 0 : 1;
