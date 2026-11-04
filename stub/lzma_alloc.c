/* Bump allocator for LZMA SDK's ISzAlloc interface - no real malloc/free
 * in a freestanding build. Reset before each segment so the arena reuses
 * the same space rather than growing across the whole file. */

#include "lzma/7zTypes.h"

#define ARENA_SIZE (20 * 1024)

static unsigned char arena[ARENA_SIZE];
static unsigned int arena_used;

static void *lzma_alloc(ISzAllocPtr p, size_t size)
{
    (void)p;
    if (arena_used + size > ARENA_SIZE)
        return 0;
    void *ptr = &arena[arena_used];
    arena_used += (unsigned int)size;
    return ptr;
}

static void lzma_free(ISzAllocPtr p, void *address)
{
    (void)p;
    (void)address;
}

const ISzAlloc g_lzmaAlloc = { lzma_alloc, lzma_free };

void lzma_alloc_reset(void)
{
    arena_used = 0;
}
