#include <axsdk/axparameter.h>
#include <gio/gio.h>
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
/* Loopback port for the settings fallback server. Must be unique per ACAP:
 * several of these VPN apps can run on one device and a shared port would make
 * one app's reverseProxy hit another app's server. Tailscale 2201,
 * ZeroTier 2202, NetBird status API 2205, NetBird settings 2206. */
#define SETTINGS_HTTP_PORT 2206

static AXParameter *parameter_handle;
static pid_t child_pid = -1;
static guint restart_timer;
static char *management_url;
static char *setup_key;
static char *http_proxy_port;
static char *socks5_port;
static char *forward_ports;
static char *inbound_socks5_port;

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
    load_value("ForwardPorts", &forward_ports);
    load_value("InboundSocks5Port", &inbound_socks5_port);
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
    write_shell_value(file, "FORWARD_PORTS", value_or(forward_ports, "80,443,554"));
    write_shell_value(file, "INBOUND_SOCKS5_PORT", value_or(inbound_socks5_port, "1080"));
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
    else if (strcmp(short_name, "ForwardPorts") == 0)
        replace_value(&forward_ports, value);
    else if (strcmp(short_name, "InboundSocks5Port") == 0)
        replace_value(&inbound_socks5_port, value);
    if (restart_timer)
        g_source_remove(restart_timer);
    restart_timer = g_timeout_add(300, restart_child, NULL);
}

/* ── settings HTTP fallback (devices without param.cgi) ──────────────
 * Recorder/NVR- and access-control-class devices do not expose the legacy
 * /axis-cgi/param.cgi VAPIX endpoint, so the web UI cannot read or write
 * parameters through it. This tiny server, reached through the manifest
 * reverseProxy mapping at /local/NetBird_VPN/config/settings, exposes the same
 * parameters. It lives in the bridge rather than the Go daemon because the
 * daemon refuses to start until a setup key is enrolled, and the UI must be
 * able to write that key. */

static const char *settings_params[] = {"ManagementURL", "SetupKey", "HTTPProxyPort",
                                        "Socks5Port", "ForwardPorts", "InboundSocks5Port"};

static int settings_is_known(const char *name) {
    for (size_t index = 0; index < G_N_ELEMENTS(settings_params); index++)
        if (strcmp(name, settings_params[index]) == 0)
            return 1;
    return 0;
}

static void settings_json_escape(GString *out, const char *value) {
    for (const char *cursor = value; *cursor; cursor++) {
        switch (*cursor) {
        case '"':
            g_string_append(out, "\\\"");
            break;
        case '\\':
            g_string_append(out, "\\\\");
            break;
        case '\n':
            g_string_append(out, "\\n");
            break;
        case '\r':
            g_string_append(out, "\\r");
            break;
        case '\t':
            g_string_append(out, "\\t");
            break;
        default:
            if ((unsigned char)*cursor < 0x20)
                g_string_append_printf(out, "\\u%04x", (unsigned char)*cursor);
            else
                g_string_append_c(out, *cursor);
        }
    }
}

static gchar *settings_build_json(void) {
    GString *out = g_string_new("{");
    gboolean first = TRUE;
    for (size_t index = 0; index < G_N_ELEMENTS(settings_params); index++) {
        const char *name = settings_params[index];
        if (strcmp(name, "SetupKey") == 0)
            continue; /* write-only: never hand the enrollment secret back out */
        gchar *value = NULL;
        GError *error = NULL;
        if (!ax_parameter_get(parameter_handle, name, &value, &error)) {
            if (error)
                g_error_free(error);
            value = g_strdup("");
        }
        if (!first)
            g_string_append_c(out, ',');
        first = FALSE;
        g_string_append_printf(out, "\"%s\":\"", name);
        settings_json_escape(out, value ? value : "");
        g_string_append_c(out, '"');
        g_free(value);
    }
    g_string_append_c(out, '}');
    /* Copy out and free fully to stay portable across glib versions. */
    gchar *json = g_strdup(out->str);
    g_string_free(out, TRUE);
    return json;
}

static gchar *settings_url_decode(const char *value, size_t length) {
    GString *out = g_string_new(NULL);
    for (size_t index = 0; index < length; index++) {
        char character = value[index];
        if (character == '+') {
            g_string_append_c(out, ' ');
        } else if (character == '%' && index + 2 < length &&
                   g_ascii_isxdigit(value[index + 1]) && g_ascii_isxdigit(value[index + 2])) {
            int high = g_ascii_xdigit_value(value[index + 1]);
            int low = g_ascii_xdigit_value(value[index + 2]);
            g_string_append_c(out, (char)((high << 4) | low));
            index += 2;
        } else {
            g_string_append_c(out, character);
        }
    }
    gchar *decoded = g_strdup(out->str);
    g_string_free(out, TRUE);
    return decoded;
}

/* Apply an application/x-www-form-urlencoded body of name=value pairs to the
 * parameter store. Returns the number of parameters successfully set. */
static int settings_apply(const char *body, size_t length) {
    int applied = 0;
    size_t start = 0;
    for (size_t index = 0; index <= length; index++) {
        if (index != length && body[index] != '&')
            continue;
        size_t segment_length = index - start;
        if (segment_length > 0) {
            const char *segment = body + start;
            const char *separator = memchr(segment, '=', segment_length);
            if (separator) {
                size_t name_length = (size_t)(separator - segment);
                gchar *name = g_strndup(segment, name_length);
                gchar *value = settings_url_decode(separator + 1, segment_length - name_length - 1);
                if (settings_is_known(name)) {
                    GError *error = NULL;
                    if (ax_parameter_set(parameter_handle, name, value, TRUE, &error)) {
                        applied++;
                    } else {
                        syslog(LOG_WARNING, "settings http: set %s failed: %s", name,
                               error ? error->message : "unknown");
                        if (error)
                            g_error_free(error);
                    }
                }
                g_free(name);
                g_free(value);
            }
        }
        start = index + 1;
    }
    return applied;
}

static size_t settings_parse_content_length(const char *headers, size_t length) {
    const char *key = "content-length:";
    size_t key_length = strlen(key);
    for (size_t index = 0; index + key_length <= length; index++) {
        if (g_ascii_strncasecmp(headers + index, key, key_length) != 0)
            continue;
        index += key_length;
        while (index < length && (headers[index] == ' ' || headers[index] == '\t'))
            index++;
        return (size_t)strtoul(headers + index, NULL, 10);
    }
    return 0;
}

static void settings_send(GOutputStream *out, const char *status, const char *content_type,
                          const char *body) {
    gchar *response = g_strdup_printf("HTTP/1.1 %s\r\n"
                                      "Content-Type: %s\r\n"
                                      "Content-Length: %zu\r\n"
                                      "Cache-Control: no-store\r\n"
                                      "Connection: close\r\n"
                                      "\r\n"
                                      "%s",
                                      status, content_type, strlen(body), body);
    g_output_stream_write_all(out, response, strlen(response), NULL, NULL, NULL);
    g_free(response);
}

static gboolean settings_on_incoming(GSocketService *service G_GNUC_UNUSED,
                                     GSocketConnection *connection,
                                     GObject *source G_GNUC_UNUSED,
                                     gpointer unused G_GNUC_UNUSED) {
    GInputStream *in = g_io_stream_get_input_stream(G_IO_STREAM(connection));
    GOutputStream *out = g_io_stream_get_output_stream(G_IO_STREAM(connection));

    GString *request = g_string_new(NULL);
    char buffer[2048];
    gboolean have_headers = FALSE;
    size_t header_end = 0;
    size_t content_length = 0;

    while (1) {
        gssize count = g_input_stream_read(in, buffer, sizeof(buffer), NULL, NULL);
        if (count <= 0)
            break;
        g_string_append_len(request, buffer, count);
        if (!have_headers) {
            char *end = g_strstr_len(request->str, request->len, "\r\n\r\n");
            if (end) {
                have_headers = TRUE;
                header_end = (size_t)(end - request->str) + 4;
                content_length = settings_parse_content_length(request->str, header_end);
            }
        }
        if (have_headers && request->len - header_end >= content_length)
            break;
        if (request->len > 262144)
            break; /* safety cap */
    }

    gboolean is_get = FALSE, is_post = FALSE, is_settings = FALSE;
    if (have_headers) {
        is_get = g_str_has_prefix(request->str, "GET ");
        is_post = g_str_has_prefix(request->str, "POST ");
        const char *space = strchr(request->str, ' ');
        if (space) {
            const char *path = space + 1;
            const char *path_end = strchr(path, ' ');
            size_t path_length = path_end ? (size_t)(path_end - path) : strlen(path);
            const char *query = memchr(path, '?', path_length);
            size_t match_length = query ? (size_t)(query - path) : path_length;
            if (match_length >= 8 &&
                g_ascii_strncasecmp(path + match_length - 8, "settings", 8) == 0)
                is_settings = TRUE;
        }
    }

    if (is_settings && is_get) {
        gchar *json = settings_build_json();
        settings_send(out, "200 OK", "application/json", json);
        g_free(json);
    } else if (is_settings && is_post) {
        const char *body = request->str + header_end;
        size_t body_length = request->len - header_end;
        if (body_length > content_length)
            body_length = content_length;
        int applied = settings_apply(body, body_length);
        syslog(LOG_INFO, "settings http: applied %d parameter(s)", applied);
        if (restart_timer)
            g_source_remove(restart_timer);
        restart_timer = g_timeout_add(300, restart_child, NULL);
        settings_send(out, "200 OK", "text/plain", "OK");
    } else {
        settings_send(out, "404 Not Found", "text/plain", "Not found");
    }

    g_string_free(request, TRUE);
    g_io_stream_close(G_IO_STREAM(connection), NULL, NULL);
    return TRUE;
}

static void settings_http_start(void) {
    GError *error = NULL;
    GSocketService *service = g_socket_service_new();
    GInetAddress *address = g_inet_address_new_from_string("127.0.0.1");
    GSocketAddress *socket_address = g_inet_socket_address_new(address, SETTINGS_HTTP_PORT);

    if (!g_socket_listener_add_address(G_SOCKET_LISTENER(service), socket_address,
                                       G_SOCKET_TYPE_STREAM, G_SOCKET_PROTOCOL_TCP, NULL, NULL,
                                       &error)) {
        syslog(LOG_WARNING, "settings http: bind 127.0.0.1:%d failed: %s", SETTINGS_HTTP_PORT,
               error ? error->message : "unknown");
        if (error)
            g_error_free(error);
        g_object_unref(service);
    } else {
        g_signal_connect(service, "incoming", G_CALLBACK(settings_on_incoming), NULL);
        g_socket_service_start(service);
        syslog(LOG_INFO, "settings http server listening on 127.0.0.1:%d", SETTINGS_HTTP_PORT);
    }
    g_object_unref(address);
    g_object_unref(socket_address);
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
    ensure_parameter("ForwardPorts", "80,443,554");
    ensure_parameter("InboundSocks5Port", "1080");
    unlink(SETUP_KEY_SENTINEL);
    load_configuration();
    write_configuration();
    start_child();

    const char *names[] = {"ManagementURL", "SetupKey", "HTTPProxyPort", "Socks5Port",
                           "ForwardPorts", "InboundSocks5Port"};
    for (size_t index = 0; index < sizeof(names) / sizeof(names[0]); index++) {
        if (!ax_parameter_register_callback(parameter_handle, names[index],
                                             parameter_changed, NULL, &error) && error) {
            syslog(LOG_WARNING, "callback %s failed: %s", names[index], error->message);
            g_error_free(error);
            error = NULL;
        }
    }

    settings_http_start();

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
    free(forward_ports);
    free(inbound_socks5_port);
    closelog();
    return 0;
}
