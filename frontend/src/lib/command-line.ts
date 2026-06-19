export interface CommandLine {
  isCommand: boolean;
  word: string;
  afterCommandName: boolean;
}

/** Parse the composer text to decide command-autocomplete state. */
export function parseCommandLine(text: string): CommandLine {
  if (!text.startsWith('/')) {
    return { isCommand: false, word: '', afterCommandName: false };
  }
  const rest = text.slice(1);
  const spaceIdx = rest.indexOf(' ');
  if (spaceIdx === -1) {
    return { isCommand: true, word: rest, afterCommandName: false };
  }
  return { isCommand: true, word: rest.slice(0, spaceIdx), afterCommandName: true };
}
