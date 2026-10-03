#define _GNU_SOURCE
#include <dlfcn.h>
#include <gio/gio.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef GSubprocess *(*spawnv_fn)(GSubprocessLauncher *, const gchar *const *, GError **);

GSubprocess *g_subprocess_launcher_spawnv(GSubprocessLauncher *launcher,
                                          const gchar *const *argv, GError **error)
{
    static spawnv_fn original;
    if (!original)
        original = (spawnv_fn)dlsym(RTLD_NEXT, "g_subprocess_launcher_spawnv");

    const char *appdir = getenv("APPDIR");
    if (!appdir || !argv || !argv[0])
        return original(launcher, argv, error);

    size_t count = 0;
    while (argv[count])
        count++;
    size_t helper_index = 0;
    for (size_t i = 0; i < count; i++) {
        if (strstr(argv[i], "/webkitgtk-6.0/WebKitNetworkProcess") ||
            strstr(argv[i], "/webkitgtk-6.0/WebKitWebProcess")) {
            helper_index = i;
            break;
        }
    }
    if (!argv[helper_index] || !strstr(argv[helper_index], "/webkitgtk-6.0/WebKit"))
        return original(launcher, argv, error);

    char helper[PATH_MAX];
    if (snprintf(helper, sizeof(helper), "%s%s", appdir, argv[helper_index]) >= (int)sizeof(helper) ||
        access(helper, X_OK) != 0)
        return original(launcher, argv, error);

    if (helper_index == 0) {
        const gchar **adjusted = g_newa(const gchar *, count + 1);
        memcpy(adjusted, argv, (count + 1) * sizeof(*adjusted));
        adjusted[0] = helper;
        return original(launcher, adjusted, error);
    }

    size_t separator = 0;
    while (separator < count && strcmp(argv[separator], "--"))
        separator++;
    if (separator == count)
        return original(launcher, argv, error);
    const gchar **adjusted = g_newa(const gchar *, count + 4);
    memcpy(adjusted, argv, separator * sizeof(*adjusted));
    adjusted[separator] = "--ro-bind";
    adjusted[separator + 1] = appdir;
    adjusted[separator + 2] = appdir;
    memcpy(adjusted + separator + 3, argv + separator, (count - separator + 1) * sizeof(*adjusted));
    adjusted[helper_index + 3] = helper;
    return original(launcher, adjusted, error);
}
