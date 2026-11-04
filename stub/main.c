/*
 * ps2pack stub body. Reached via "jal main" from crt0.s with argc/argv in
 * a0/a1. Unpacks every segment (LZMA1, via the vendored LzmaDec.c) into
 * place and chain-execs the original program.
 */

#include "lzma/LzmaDec.h"

typedef unsigned char u8;
typedef unsigned int u32;

/* Defined in crt0.s, not here, so they land at a fixed offset before the
 * entry point (see crt0.s). g_payload_ptr is patched by ps2pack. */
extern volatile u32 g_codec_tag;
extern volatile u32 g_payload_ptr;

typedef struct {
    u32 original_entry;
    u32 segment_count;
} PackedHeader;

typedef struct {
    u32 decompressed_size;
    u32 bss_size;
    u32 load_addr;
    u32 compressed_size; /* includes the 5-byte LZMA props header */
} PackedSegment;

/* Freestanding build: no libc. LzmaDec.c needs these. */
void *memcpy(void *dst, const void *src, unsigned int n)
{
    u8 *d = (u8 *)dst;
    const u8 *s = (const u8 *)src;
    while (n--)
        *d++ = *s++;
    return dst;
}

void *memset(void *dst, int c, unsigned int n)
{
    u8 *d = (u8 *)dst;
    while (n--)
        *d++ = (u8)c;
    return dst;
}

extern const ISzAlloc g_lzmaAlloc;
extern void lzma_alloc_reset(void);

static void flush_cache(int mode)
{
    __asm__ __volatile__(
        "li $3, 0x64\n"
        "move $4, %0\n"
        "syscall\n"
        :
        : "r"(mode)
        : "$2", "$3", "$4");
}

static void halt(void) __attribute__((noreturn));
static void halt(void)
{
    for (;;) {
        __asm__ __volatile__(
            "li $3, 0x32\n" /* __NR_SleepThread */
            "syscall\n" ::
                : "$2", "$3");
    }
}

extern u8 stub_args[];

static void exec_ps2(u32 entry, int argc, char **argv) __attribute__((noreturn));
static void exec_ps2(u32 entry, int argc, char **argv)
{
    register u32 a0 __asm__("$4") = entry;
    register u32 a1 __asm__("$5") = 0;
    register int a2 __asm__("$6") = argc;
    register char **a3 __asm__("$7") = argv;
    __asm__ __volatile__(
        "li $3, 7\n" /* __NR_ExecPS2 */
        "syscall\n"
        :
        : "r"(a0), "r"(a1), "r"(a2), "r"(a3)
        : "$2", "$3", "memory");
    for (;;) {
    }
}

void main(int argc, char **argv) __attribute__((noreturn));

void main(int argc, char **argv)
{
    PackedHeader *hdr = (PackedHeader *)g_payload_ptr;
    u8 *cursor = (u8 *)(hdr + 1);

    for (u32 i = 0; i < hdr->segment_count; i++) {
        PackedSegment *seg = (PackedSegment *)cursor;
        cursor += sizeof(PackedSegment);

        SizeT srcLen = seg->compressed_size - LZMA_PROPS_SIZE;
        SizeT destLen = seg->decompressed_size;
        ELzmaStatus status;

        lzma_alloc_reset();
        SRes res = LzmaDecode((Byte *)seg->load_addr, &destLen, cursor + LZMA_PROPS_SIZE, &srcLen,
                               cursor, LZMA_PROPS_SIZE, LZMA_FINISH_END, &status, &g_lzmaAlloc);
        if (res != SZ_OK)
            halt();

        if (seg->bss_size != 0)
            memset((u8 *)seg->load_addr + seg->decompressed_size, 0, seg->bss_size);

        /* Next segment header starts 4-byte-aligned relative to the
         * container, matching the host tool's padding. */
        cursor += seg->compressed_size;
        cursor = (u8 *)(((u32)cursor + 3) & ~3u); // NOTE: this alignment is incredibly important. If this part is skipped the app will crash.
    }

    flush_cache(0); /* writeback dcache */
    flush_cache(2); /* invalidate icache so the code we just wrote is visible */

    /* crt0.s's SetupThread made the kernel store the launch arguments in stub_args (behind a pid
     * word); hand them to ExecPS2, which copies them for the payload's own start-up. The payload can
     * have overwritten the memory the kernel originally held them in. */
    int *sargs = (int *)(stub_args + 4);
    int launch_argc = sargs[0];
    exec_ps2(hdr->original_entry, launch_argc, launch_argc > 0 ? (char **)&sargs[1] : 0);
    halt();
}
