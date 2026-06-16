#ifndef CASCADE_WINDOW_TABBING_DARWIN_H
#define CASCADE_WINDOW_TABBING_DARWIN_H

// disableAutomaticWindowTabbing opts the whole process out of macOS automatic
// window tabbing (NSWindow.allowsAutomaticWindowTabbing). Must be called on the
// main thread after NSApplication has launched.
void disableAutomaticWindowTabbing(void);

#endif // CASCADE_WINDOW_TABBING_DARWIN_H
