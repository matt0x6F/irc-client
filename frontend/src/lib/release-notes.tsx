import type { ReactNode } from 'react';

// Renders the *trusted* subset of GitHub-flavored Markdown that appears in a
// release body (the `notes` shown by UpdateAvailableDialog): headings, bullet
// lists, bold, inline code, `[text](url)` links, and bare http(s) URLs.
//
// Why a hand-rolled renderer instead of a markdown dependency: the input is
// small, comes from our own GitHub releases over HTTPS, and only uses a handful
// of constructs (see the auto-generated "## What's Changed" / "**Full
// Changelog**" template). We build React elements directly — never
// dangerouslySetInnerHTML — so there is no HTML-injection surface, and links are
// filtered to http(s) so a crafted `javascript:` URL can't be emitted. Real
// <a href> anchors are used so the app-wide delegated handler
// (installExternalLinkHandler) opens them in the system browser for free.

// [text](url) OR a bare http(s) URL. Both url capture groups are constrained to
// http(s) so nothing else can become a link.
const LINK_RE = /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)|(https?:\/\/[^\s<]+)/g;

// **bold** OR `code`. Applied only to the plain-text gaps between links.
const EMPHASIS_RE = /\*\*([^*]+)\*\*|`([^`]+)`/g;

// Renders **bold** and `code` within a run of text that contains no links.
function renderEmphasis(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const re = new RegExp(EMPHASIS_RE); // fresh lastIndex per call
  let last = 0;
  let i = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) nodes.push(text.slice(last, m.index));
    if (m[1] !== undefined) {
      nodes.push(
        <strong key={`${keyPrefix}-b${i}`} className="font-semibold text-foreground">
          {m[1]}
        </strong>,
      );
    } else {
      nodes.push(
        <code key={`${keyPrefix}-c${i}`} className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]">
          {m[2]}
        </code>,
      );
    }
    last = m.index + m[0].length;
    i++;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return nodes;
}

// Renders links first (so URL contents aren't mangled by emphasis parsing), then
// hands the surrounding text to renderEmphasis.
function renderInline(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const re = new RegExp(LINK_RE);
  let last = 0;
  let i = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) nodes.push(...renderEmphasis(text.slice(last, m.index), `${keyPrefix}-t${i}`));
    const url = m[2] ?? m[3];
    const label = m[1] ?? m[3];
    nodes.push(
      <a
        key={`${keyPrefix}-a${i}`}
        href={url}
        className="text-primary underline underline-offset-2 hover:no-underline"
      >
        {label}
      </a>,
    );
    last = m.index + m[0].length;
    i++;
  }
  if (last < text.length) nodes.push(...renderEmphasis(text.slice(last), `${keyPrefix}-t${i}`));
  return nodes;
}

const HEADING_RE = /^(#{1,6})\s+(.*)$/;
const BULLET_RE = /^[-*+]\s+(.*)$/;

// Parses release-note markdown into block-level React elements. Blank lines
// separate blocks; consecutive non-blank, non-bullet lines join into a single
// paragraph (matching how markdown reflows soft line breaks).
export function renderReleaseNotes(markdown: string): ReactNode {
  const lines = markdown.replace(/\r\n/g, '\n').split('\n');
  const blocks: ReactNode[] = [];
  let paragraph: string[] = [];
  let list: string[] = [];
  let key = 0;

  const flushParagraph = () => {
    if (paragraph.length === 0) return;
    const text = paragraph.join(' ');
    blocks.push(
      <p key={`p${key}`} className="leading-relaxed">
        {renderInline(text, `p${key}`)}
      </p>,
    );
    paragraph = [];
    key++;
  };

  const flushList = () => {
    if (list.length === 0) return;
    const items = list;
    blocks.push(
      <ul key={`ul${key}`} className="list-disc space-y-1 pl-5">
        {items.map((item, idx) => (
          <li key={idx}>{renderInline(item, `ul${key}-${idx}`)}</li>
        ))}
      </ul>,
    );
    list = [];
    key++;
  };

  for (const raw of lines) {
    const line = raw.trimEnd();
    if (line.trim() === '') {
      flushParagraph();
      flushList();
      continue;
    }
    const heading = HEADING_RE.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      blocks.push(
        <p key={`h${key}`} className="mt-1 font-semibold text-foreground">
          {renderInline(heading[2], `h${key}`)}
        </p>,
      );
      key++;
      continue;
    }
    const bullet = BULLET_RE.exec(line);
    if (bullet) {
      flushParagraph();
      list.push(bullet[1]);
      continue;
    }
    flushList();
    paragraph.push(line.trim());
  }
  flushParagraph();
  flushList();

  return blocks;
}
