PROG = NetBird_VPN
SRCS = param_bridge.c
PKGS = axparameter glib-2.0 gio-2.0

CFLAGS += -Wall -Wextra -Werror $(shell pkg-config --cflags $(PKGS))
LDLIBS += $(shell pkg-config --libs $(PKGS))

all: $(PROG)

$(PROG): $(SRCS)
	$(CC) $(CFLAGS) -o $@ $^ $(LDLIBS)

clean:
	rm -f $(PROG)
