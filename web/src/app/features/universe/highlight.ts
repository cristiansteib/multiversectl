import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import css from 'highlight.js/lib/languages/css';
import dockerfile from 'highlight.js/lib/languages/dockerfile';
import go from 'highlight.js/lib/languages/go';
import ini from 'highlight.js/lib/languages/ini';
import java from 'highlight.js/lib/languages/java';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import kotlin from 'highlight.js/lib/languages/kotlin';
import markdown from 'highlight.js/lib/languages/markdown';
import php from 'highlight.js/lib/languages/php';
import python from 'highlight.js/lib/languages/python';
import ruby from 'highlight.js/lib/languages/ruby';
import rust from 'highlight.js/lib/languages/rust';
import scss from 'highlight.js/lib/languages/scss';
import sql from 'highlight.js/lib/languages/sql';
import typescript from 'highlight.js/lib/languages/typescript';
import xml from 'highlight.js/lib/languages/xml';
import yaml from 'highlight.js/lib/languages/yaml';

/**
 * Colouring the code in a diff by the language it is written in.
 *
 * Only the languages below are registered, from highlight.js's own list rather
 * than its "all" bundle: the full set is an order of magnitude larger and this
 * is a panel served off a developer's own machine, not a public page, so the
 * cost is paid once per load but there is no reason to pay a needless one.
 * Adding a language is one import and one line in BY_EXT.
 *
 * Nothing is auto-detected. highlight.js can guess, but a guess on one line of
 * a diff is wrong often enough to be worse than plain text — and the file's
 * name is right there, so the extension decides.
 */
const LANGS: Record<string, unknown> = {
  bash, css, dockerfile, go, ini, java, javascript, json, kotlin, markdown,
  php, python, ruby, rust, scss, sql, typescript, xml, yaml,
};
for (const [name, def] of Object.entries(LANGS)) {
  hljs.registerLanguage(name, def as never);
}

/** The language a file's name implies, or null to leave it uncoloured. */
const BY_EXT: Record<string, string> = {
  ts: 'typescript', tsx: 'typescript', mts: 'typescript', cts: 'typescript',
  js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  go: 'go', py: 'python', rb: 'ruby', rs: 'rust', java: 'java', kt: 'kotlin',
  php: 'php', sql: 'sql', css: 'css', scss: 'scss', sass: 'scss',
  html: 'xml', htm: 'xml', xml: 'xml', svg: 'xml', vue: 'xml',
  json: 'json', jsonc: 'json', yml: 'yaml', yaml: 'yaml',
  md: 'markdown', markdown: 'markdown',
  sh: 'bash', bash: 'bash', zsh: 'bash', env: 'bash',
  toml: 'ini', ini: 'ini', cfg: 'ini', conf: 'ini',
};

/** Files whose whole name, not extension, says what they are. */
const BY_NAME: Record<string, string> = {
  dockerfile: 'dockerfile', makefile: 'bash', '.gitignore': 'bash',
  '.env': 'bash', 'go.mod': 'ini', 'go.sum': 'ini',
};

export function languageOf(path: string): string | null {
  const name = (path.split('/').pop() ?? '').toLowerCase();
  if (BY_NAME[name]) return BY_NAME[name];
  if (name.startsWith('dockerfile')) return 'dockerfile';
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : '';
  return BY_EXT[ext] ?? null;
}

/**
 * One line of code as HTML.
 *
 * Each line is highlighted on its own, which is the compromise this makes
 * knowingly. A diff is not a file: it has no beginning, its hunks skip whole
 * regions, and its removed and added lines never both belong to the same
 * document, so there is no single text to hand a highlighter. Line by line a
 * block comment or a template literal spanning several lines is coloured as
 * though each line stood alone, and `ignoreIllegals` keeps that from throwing.
 * Everything else — keywords, strings, numbers, types, calls — is right, which
 * is what the colour is for.
 *
 * The output is HTML and the input is somebody's file, so this is the one place
 * in the panel where that matters: highlight.js escapes the text it emits, and
 * the classes it adds are its own fixed set. Nothing from the file reaches the
 * markup unescaped. Returns null when the language is unknown, so the caller
 * can go on printing text.
 */
export function highlightLine(text: string, language: string | null): string | null {
  if (!language || !text) return null;
  try {
    return hljs.highlight(text, { language, ignoreIllegals: true }).value;
  } catch {
    return null;   // an unregistered language, or anything else: plain text
  }
}
