CC = gcc
CFLAGS = -O -Wall -Wextra -Wno-unused-parameter -fPIC
LDFLAGS = -shared
TARGET = fake_libhlml.so
SRCS = $(wildcard *.c)
OBJS = $(SRCS:.c=.o)

all: $(TARGET)

$(TARGET): $(OBJS)
	$(CC) $(LDFLAGS) -I /usr/include/habanalabs/ -o $@ $^

%.o: %.c
	$(CC) $(CFLAGS) -I /usr/include/habanalabs/ -c $< -o $@

clean:
	rm -f $(OBJS) $(TARGET)

.PHONY: all clean
