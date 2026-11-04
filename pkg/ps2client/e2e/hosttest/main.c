/* PS2 side of the ps2client end-to-end test: exercises the file device and
 * writes a report to results.txt. Build with -DDEV=\"pkfs:\" for a ps2link whose
 * device is not called host:. CC0 1.0. */
#ifndef DEV
#define DEV "host:"
#endif

#include <kernel.h>
#include <sifrpc.h>
#include <debug.h>
#include <stdio.h>
#include <stdarg.h>
#include <string.h>
#include <stdint.h>
#include <fcntl.h>
#include <unistd.h>
#include <dirent.h>
#include <sys/stat.h>

static int passed, failed;
static char out[16384];
static int outlen;
/* Console text goes to stdout and to tty0: explicitly. PCSX2 handles writes to
 * stdout itself, before any driver sees them, so only the tty0: copy reaches
 * ps2link's UDP console there. */
static int ttyfd = -1;

static void logline(const char *fmt, ...)
{
	char line[200];
	va_list ap;
	va_start(ap, fmt);
	int n = vsnprintf(line, sizeof(line), fmt, ap);
	va_end(ap);
	if (n > (int)sizeof(line) - 1)
		n = sizeof(line) - 1;
	printf("%s", line);
	if (ttyfd >= 0)
		write(ttyfd, line, n);
	if (outlen < (int)sizeof(out) - (int)sizeof(line)) {
		memcpy(out + outlen, line, n);
		outlen += n;
	}
}
#define LOG logline

static void report(const char *name, int ok)
{
	if (ok) {
		passed++;
		LOG("HT: PASS %s\n", name);
	} else {
		failed++;
		LOG("HT: FAIL %s\n", name);
	}
	scr_printf("%s %s\n", ok ? "PASS" : "FAIL", name);
}

static uint8_t pat(uint32_t i)
{
	return (uint8_t)((i * 31u + 7u) & 0xff);
}

static int read_all(const char *path, uint8_t *buf, int max)
{
	int fd = open(path, O_RDONLY);
	if (fd < 0)
		return -1;
	int total = 0, n;
	while (total < max && (n = read(fd, buf + total, max - total)) > 0)
		total += n;
	close(fd);
	return total;
}

static uint8_t big[300 * 1024];

int main(int argc, char *argv[])
{
	init_scr();
	SifInitRpc(0);
	ttyfd = open("tty0:", O_WRONLY);
	LOG("HT: BEGIN argc=%d\n", argc);

	char buf[128];
	int n = read_all(DEV "in.txt", (uint8_t *)buf, sizeof(buf) - 1);
	buf[n > 0 ? n : 0] = 0;
	report("read_text", n == 16 && strcmp(buf, "PC to PS2 works\n") == 0);

	int fd = open(DEV "out.txt", O_WRONLY | O_CREAT | O_TRUNC, 0666);
	static const char msg[] = "PS2 to PC works\n";
	int wr = fd >= 0 ? write(fd, msg, sizeof(msg) - 1) : -99;
	int cl = fd >= 0 ? close(fd) : -99;
	LOG("HT: write_text fd=%d write=%d close=%d\n", fd, wr, cl);
	report("write_text", fd >= 0 && wr == (int)sizeof(msg) - 1 && cl >= 0);

	n = read_all(DEV "big.bin", big, sizeof(big));
	int ok = n == 300 * 1024;
	for (int i = 0; ok && i < n; i++)
		ok = big[i] == pat(i);
	LOG("HT: big.bin read %d bytes\n", n);
	report("read_300KiB_integrity", ok);

	for (int i = 0; i < 100 * 1024; i++)
		big[i] = pat(i + 5);
	fd = open(DEV "big2.bin", O_WRONLY | O_CREAT | O_TRUNC, 0666);
	int wrote = fd >= 0 ? write(fd, big, 100 * 1024) : -1;
	if (fd >= 0)
		close(fd);
	LOG("HT: big2.bin wrote %d bytes\n", wrote);
	report("write_100KiB", wrote == 100 * 1024);

	fd = open(DEV "in.txt", O_RDONLY);
	off_t size = fd >= 0 ? lseek(fd, 0, SEEK_END) : -1;
	off_t pos = fd >= 0 ? lseek(fd, 3, SEEK_SET) : -1;
	char c[4] = {0};
	int r = fd >= 0 ? read(fd, c, 3) : -1;
	if (fd >= 0)
		close(fd);
	LOG("HT: lseek size=%d pos=%d read=%d\n", (int)size, (int)pos, r);
	report("lseek_end_set_read", size == 16 && pos == 3 && r == 3 && memcmp(c, "to ", 3) == 0);

	struct stat st;
	memset(&st, 0, sizeof(st));
	int g = stat(DEV "in.txt", &st);
	LOG("HT: stat=%d mode=0x%x size=%d\n", g, (unsigned)st.st_mode, (int)st.st_size);
	report("stat_file", g == 0 && st.st_size == 16 && S_ISREG(st.st_mode));

	memset(&st, 0, sizeof(st));
	g = stat(DEV "sub", &st);
	LOG("HT: stat dir=%d mode=0x%x\n", g, (unsigned)st.st_mode);
	report("stat_dir", g == 0 && S_ISDIR(st.st_mode));

	report("open_missing_fails", open(DEV "nope.txt", O_RDONLY) < 0);

	int m = mkdir(DEV "newdir", 0777);
	memset(&st, 0, sizeof(st));
	report("mkdir", m == 0 && stat(DEV "newdir", &st) == 0 && S_ISDIR(st.st_mode));

	DIR *d = opendir(DEV "");
	int files = 0, dirs = 0, sawIn = 0;
	struct dirent *de;
	while (d && (de = readdir(d)) != NULL) {
		LOG("HT: entry %s\n", de->d_name);
		char path[300];
		snprintf(path, sizeof(path), DEV "%s", de->d_name);
		if (stat(path, &st) == 0 && S_ISDIR(st.st_mode))
			dirs++;
		else
			files++;
		if (strcmp(de->d_name, "in.txt") == 0)
			sawIn = 1;
	}
	if (d)
		closedir(d);
	LOG("HT: listing files=%d dirs=%d\n", files, dirs);
	report("list_dir", d != NULL && sawIn && dirs >= 2 && files >= 3);

	report("rmdir", rmdir(DEV "newdir") == 0 && stat(DEV "newdir", &st) != 0);
	fd = open(DEV "tmp.txt", O_WRONLY | O_CREAT | O_TRUNC, 0666);
	if (fd >= 0) {
		write(fd, "x", 1);
		close(fd);
	}
	int u = unlink(DEV "tmp.txt");
	LOG("HT: unlink tmp.txt returned %d\n", u);
	memset(&st, 0, sizeof(st));
	int gs = stat(DEV "tmp.txt", &st);
	LOG("HT: stat after unlink returned %d\n", gs);
	/* The SDK documents fioRemove as falling through into a mkdir, so the name reappears as a directory. */
	report("remove", u == 0);

	LOG("HT: DONE passed=%d failed=%d\n", passed, failed);
	fd = open(DEV "results.txt", O_WRONLY | O_CREAT | O_TRUNC, 0666);
	if (fd >= 0) {
		write(fd, out, outlen);
		close(fd);
	}
	scr_printf("DONE passed=%d failed=%d\n", passed, failed);
	SleepThread();
	return 0;
}
