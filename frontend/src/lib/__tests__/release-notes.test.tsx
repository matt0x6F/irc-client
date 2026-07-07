import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { renderReleaseNotes } from '../release-notes';

// Renders the ReactNode output into a container for assertions.
function renderNotes(md: string) {
  return render(<div>{renderReleaseNotes(md)}</div>);
}

describe('renderReleaseNotes', () => {
  it('renders a heading as bold text, not literal "##"', () => {
    const { container } = renderNotes('## What\'s Changed');
    const heading = container.querySelector('.font-semibold');
    expect(heading).toBeInTheDocument();
    expect(heading).toHaveTextContent("What's Changed");
    expect(container.textContent).not.toContain('##');
  });

  it('renders "* item" lines as a bullet list, not literal "*"', () => {
    const { container } = renderNotes('* first\n* second');
    const items = container.querySelectorAll('li');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('first');
    expect(items[1]).toHaveTextContent('second');
    expect(container.textContent).not.toContain('*');
  });

  it('renders **bold** without the asterisks', () => {
    const { container } = renderNotes('**Full Changelog**: see below');
    const strong = container.querySelector('strong');
    expect(strong).toHaveTextContent('Full Changelog');
    expect(container.textContent).toBe('Full Changelog: see below');
  });

  it('renders a [text](url) link as an anchor with the http href', () => {
    const { container } = renderNotes('by [matt](https://github.com/matt0x6F) in PR');
    const link = container.querySelector('a');
    expect(link).toHaveAttribute('href', 'https://github.com/matt0x6F');
    expect(link).toHaveTextContent('matt');
  });

  it('autolinks a bare http(s) URL, including compare URLs with "..."', () => {
    const url = 'https://github.com/matt0x6F/irc-client/compare/v26.7.2-rc.122...v26.7.2-rc.123';
    const { container } = renderNotes(url);
    const link = container.querySelector('a');
    expect(link).toHaveAttribute('href', url);
    expect(link).toHaveTextContent(url);
  });

  it('never emits a non-http (e.g. javascript:) link', () => {
    const { container } = renderNotes('[click](javascript:alert(1))');
    // The malformed link isn't matched, so no anchor is produced.
    expect(container.querySelector('a')).toBeNull();
  });

  it('joins soft-wrapped paragraph lines and separates blocks on blank lines', () => {
    const { container } = renderNotes('line one\nline two\n\nsecond para');
    const paragraphs = container.querySelectorAll('p');
    expect(paragraphs).toHaveLength(2);
    expect(paragraphs[0]).toHaveTextContent('line one line two');
    expect(paragraphs[1]).toHaveTextContent('second para');
  });

  it('renders a realistic GitHub release body end to end', () => {
    const body = [
      'Automated pre-release from main @ 0cdfa30.',
      '',
      "## What's Changed",
      '* fix(ui): re-pin message list by @matt0x6F in https://github.com/matt0x6F/irc-client/pull/152',
      '',
      '**Full Changelog**: https://github.com/matt0x6F/irc-client/compare/v26.7.2-rc.122...v26.7.2-rc.123',
    ].join('\n');
    const { container } = renderNotes(body);

    expect(container.querySelector('.font-semibold')).toHaveTextContent("What's Changed");
    expect(container.querySelectorAll('li')).toHaveLength(1);
    // Both the PR link and the changelog link become anchors.
    expect(container.querySelectorAll('a')).toHaveLength(2);
    expect(container.textContent).not.toContain('##');
    expect(container.textContent).not.toContain('**');
  });
});
