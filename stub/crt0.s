# ps2pack stub entry point.
#
# No PS2SDK crt0 - EELOAD already sets up a valid stack/gp/thread context
# before jumping to any ELF's entry point, so SetupThread/SetupHeap would
# just be redundant. Zero our own .bss, forward a0/a1 (argc/argv)
# unchanged, jump into main().
#
# g_codec_tag/g_payload_ptr followed by >=5 NOP words before the entry
# point isn't stylistic: an EE instruction fetch adjacent to certain data
# bit patterns can be misread as a CACHE instruction and corrupt execution.

	.set noreorder
	.set noat

	.text
	.align 4

	.globl g_codec_tag
g_codec_tag:
	.word 0			# algorithm tag, unused in this build
	.globl g_payload_ptr
g_payload_ptr:
	.word 0			# patched by ps2pack at pack time
	nop
	nop
	nop
	nop
	nop

	.globl _stub_start
	.ent _stub_start
_stub_start:
	move	$16, $4		# s0 = a0
	move	$17, $5		# s1 = a1

	la	$2, _fbss
	la	$3, _end
1:
	sltu	$1, $2, $3
	beq	$1, $0, 2f
	nop
	sq	$0, ($2)
	addiu	$2, $2, 16
	j	1b
	nop
2:

	# Move to a stack at the top of RAM before unpacking: the launcher's own
	# stack can sit in low memory, which a payload linked below 1 MB overwrites.
	la	$4, _gp
	li	$5, -1
	li	$6, 0x20000
	la	$7, stub_args+4
	la	$8, stub_exit
	li	$3, 60		# SetupThread
	syscall
	move	$29, $2
	ei

	move	$4, $16
	move	$5, $17
	jal	main
	nop
	.end _stub_start

stub_exit:
	li	$3, 35		# ExitThread
	syscall
	jr	$31
	nop

	.globl	stub_args
	.lcomm	stub_args, 512
