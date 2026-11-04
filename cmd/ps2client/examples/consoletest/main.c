/* Prints with the ordinary C printf. On a real PS2 running ps2link, stdout is
 * the tty0: device that udptty sends over UDP, so `ps2client listen` (or the
 * output of `ps2client execee`) shows these lines on the PC. The lines also go
 * to the TV. CC0 1.0. */
#include <kernel.h>
#include <debug.h>
#include <stdio.h>
#include <unistd.h>

int main(int argc, char *argv[])
{
	init_scr();
	scr_printf("consoletest: watch the PC for the same lines\n");

	printf("consoletest: hello from the PS2 (argc=%d)\n", argc);
	for (int i = 0; i < argc; i++)
		printf("consoletest: argv[%d] = %s\n", i, argv[i]);

	for (int tick = 1; tick <= 20; tick++) {
		printf("consoletest: tick %d of 20\n", tick);
		scr_printf("tick %d\n", tick);
		sleep(1);
	}

	printf("consoletest: done\n");
	scr_printf("done\n");
	SleepThread();
	return 0;
}
