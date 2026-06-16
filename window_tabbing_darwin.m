#import <Cocoa/Cocoa.h>
#import "window_tabbing_darwin.h"

// See window_tabbing_darwin.go for why Cascade disables automatic window
// tabbing app-wide. NSWindow.allowsAutomaticWindowTabbing is a class-level
// (process-global) setting, so one call governs every window the app — and the
// Wails updater — creates.
//
// The main-queue hop is load-bearing, not ceremony: Wails dispatches
// application-event listeners on background goroutines (see handleApplicationEvent
// in the application package), and +[NSWindow setAllowsAutomaticWindowTabbing:]
// is an AppKit setter that is silently ignored off the main thread. Calling it
// directly from the listener leaves the flag at its YES default and the updater
// window still tab-merges; dispatching to the main queue is what makes the
// setter take. Available since macOS 10.12; the @available guard keeps the
// build warning-free regardless of deployment target.
void disableAutomaticWindowTabbing(void) {
    if (@available(macOS 10.12, *)) {
        dispatch_async(dispatch_get_main_queue(), ^{
            [NSWindow setAllowsAutomaticWindowTabbing:NO];
        });
    }
}
