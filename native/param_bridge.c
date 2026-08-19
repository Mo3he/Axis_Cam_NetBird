#include <axsdk/axparameter.h>
#include <glib-unix.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <syslog.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#define APP_NAME "NetBird_VPN"
#define CONFIG_FILE "/usr/local/packages/NetBird_VPN/localdata/params.conf"
#define RUN_SCRIPT "/usr/local/packages/NetBird_VPN/NetBird_VPN_run"
#define SETUP_KEY_SENTINEL "/usr/local/packages/NetBird_VPN/localdata/setup_key_clear"

static AXParameter *parameter_handle;
static pid_t child_pid = -1;
static guint restart_timer;
static char *management_url;
static char *setup_key;
static char *http_proxy_port;
static char *socks5_port;

static void replace_value(char **target, const char *value) {
    free(*target);
    *target = strdup(value ? value : "");
}

static const char *value_or(const char *value, const char *fallback) {
    return value && *value ? value : fallback;
}

static void ensure_parameter(const char *name, const char *default_value) {
    GError *error = NULL;
    if (!ax_parameter_add(parameter_handle, name, default_value, "string", &error) && error)
        g_error_free(error);
}

static void load_value(const char *name, char **target) {
    GError *error = NULL;
    gchar *value = NULL;
    if (ax_parameter_get(parameter_handle, name, &value, &error)) {
        replace_value(target, value);
        g_free(value);
    } else {
        syslog(LOG_WARNING, "read parameter %s failed: %s", name,
               error ? error->message : "unknown");
        if (error)
            g_error_free(error);
    }
}

static void write_shell_value(FILE *file, const char *name, const char *value) {
    fputs(name, file);
    fputs("='", file);
    for (const char *cursor = value_or(value, ""); *cursor; cursor++) {
        if (*cursor == '\'')
            fputs("'\\''", file);
        else
            fputc(*cursor, file);
    }
    fputs("'\n", file);
}

static void load_configuration(void) {
    load_value("ManagementURL", &management_url);
    load_value("SetupKey", &setup_key);
    load_value("HTTPProxyPort", &http_proxy_port);
    load_value("Socks5Port", &socks5_port);
}

static void write_configuration(void) {
    FILE *file = fopen(CONFIG_FILE, "w");
    if (!file) {
        syslog(LOG_ERR, "open %s failed", CONFIG_FILE);
        return;
    }
    write_shell_value(file, "MANAGEMENT_URL", value_or(management_url, "https://api.netbird.io:443"));
    write_shell_value(file, "SETUP_KEY", setup_key);
    write_shell_value(file, "HTTP_PROXY_PORT", value_or(http_proxy_port, "18080"));
    write_shell_value(file, "SOCKS5_PORT", value_or(socks5_port, "11080"));
    fclose(file);
    chmod(CONFIG_FILE, 0600);
}

static void stop_child(void) {
    if (child_pid <= 0)
        return;
    kill(child_pid, SIGTERM);
    for (int attempt = 0; attempt < 30; attempt++) {
        int status;
        if (waitpid(child_pid, &status, WNOHANG) == child_pid) {
            child_pid = -1;
            return;
        }
        usleep(100000);
    }
    kill(child_pid, SIGKILL);
    waitpid(child_pid, NULL, 0);
    child_pid = -1;
}

static void start_child(void) {
    stop_child();
    child_pid = fork();
    if (child_pid < 0) {
        syslog(LOG_ERR, "fork failed");
        return;
    }
    if (child_pid == 0) {
        execl(RUN_SCRIPT, RUN_SCRIPT, (char *)NULL);
        _exit(1);
    }
    syslog(LOG_INFO, "started NetBird daemon (pid %d)", child_pid);
}

static gboolean restart_child(gpointer unused) {
    (void)unused;
    restart_timer = 0;
    load_configuration();
    write_configuration();
    start_child();
    return G_SOURCE_REMOVE;
}

static void parameter_changed(const gchar *name, const gchar *value,
                              gpointer unused) {
    (void)unused;
    const char *short_name = strrchr(name, '.');
    short_name = short_name ? short_name + 1 : name;
    if (strcmp(short_name, "ManagementURL") == 0)
        replace_value(&management_url, value);
    else if (strcmp(short_name, "SetupKey") == 0)
        replace_value(&setup_key, value);
    else if (strcmp(short_name, "HTTPProxyPort") == 0)
        replace_value(&http_proxy_port, value);
    else if (strcmp(short_name, "Socks5Port") == 0)
        replace_value(&socks5_port, value);
    if (restart_timer)
        g_source_remove(restart_timer);
    restart_timer = g_timeout_add(300, restart_child, NULL);
}

static gboolean watchdog(gpointer unused) {
    (void)unused;
    if (child_pid > 0) {
        int status;
        if (waitpid(child_pid, &status, WNOHANG) == child_pid) {
            child_pid = -1;
            start_child();
        }
    }
    return G_SOURCE_CONTINUE;
}

static gboolean clear_setup_key(gpointer unused) {
    (void)unused;
    if (access(SETUP_KEY_SENTINEL, F_OK) != 0 || !setup_key || !*setup_key)
        return G_SOURCE_CONTINUE;

    GError *error = NULL;
    if (ax_parameter_set(parameter_handle, "SetupKey", "", TRUE, &error)) {
        replace_value(&setup_key, "");
        syslog(LOG_INFO, "SetupKey cleared after successful enrollment");
        unlink(SETUP_KEY_SENTINEL);
    } else if (error) {
        syslog(LOG_WARNING, "failed to clear SetupKey: %s", error->message);
        g_error_free(error);
    }
    return G_SOURCE_CONTINUE;
}

static gboolean stop_application(gpointer loop) {
    stop_child();
    g_main_loop_quit(loop);
    return G_SOURCE_REMOVE;
}

int main(void) {
    GError *error = NULL;
    openlog(APP_NAME, LOG_PID, LOG_USER);
    mkdir("/usr/local/packages/NetBird_VPN/localdata", 0755);

    parameter_handle = ax_parameter_new(APP_NAME, &error);
    if (!parameter_handle) {
        syslog(LOG_ERR, "ax_parameter_new failed: %s", error ? error->message : "unknown");
        if (error)
            g_error_free(error);
        return 1;
    }

    ensure_parameter("Socks5Port", "11080");
    unlink(SETUP_KEY_SENTINEL);
    load_configuration();
    write_configuration();
    start_child();

    const char *names[] = {"ManagementURL", "SetupKey", "HTTPProxyPort", "Socks5Port"};
    for (size_t index = 0; index < sizeof(names) / sizeof(names[0]); index++) {
        if (!ax_parameter_register_callback(parameter_handle, names[index],
                                             parameter_changed, NULL, &error) && error) {
            syslog(LOG_WARNING, "callback %s failed: %s", names[index], error->message);
            g_error_free(error);
            error = NULL;
        }
    }

    GMainLoop *loop = g_main_loop_new(NULL, FALSE);
    g_unix_signal_add(SIGTERM, stop_application, loop);
    g_unix_signal_add(SIGINT, stop_application, loop);
    g_timeout_add_seconds(10, watchdog, NULL);
    g_timeout_add_seconds(2, clear_setup_key, NULL);
    g_main_loop_run(loop);
    g_main_loop_unref(loop);
    ax_parameter_free(parameter_handle);
    free(management_url);
    free(setup_key);
    free(http_proxy_port);
    free(socks5_port);
    closelog();
    return 0;
}
