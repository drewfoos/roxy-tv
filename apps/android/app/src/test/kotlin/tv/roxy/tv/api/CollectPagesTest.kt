package tv.roxy.tv.api

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test

class CollectPagesTest {
    // A fake /v1/items like the server's: at most 2000 a request (100 without a limit).
    private fun server(n: Int, calls: MutableList<Int>): suspend (Int) -> ItemsResponse = { offset ->
        calls += offset
        val items = (offset until minOf(n, offset + ITEMS_PAGE)).map { Item(id = "e${it + 1}", type = "episode", title = "E${it + 1}") }
        ItemsResponse(items, n)
    }

    // A long show's episodes (SpongeBob: 716; a bigger library: thousands) all come back,
    // not the first page: the next episode was picked from the first 100 only.
    @Test fun readsEveryPage() = runBlocking {
        val calls = mutableListOf<Int>()
        val r = collectPages(ITEMS_PAGE, server(4500, calls))
        assertEquals(4500, r.items.size)
        assertEquals("e4500", r.items.last().id)
        assertEquals(listOf(0, 2000, 4000), calls)
    }

    @Test fun oneRequestWhenItFits() = runBlocking {
        val calls = mutableListOf<Int>()
        assertEquals(716, collectPages(ITEMS_PAGE, server(716, calls)).items.size)
        assertEquals(listOf(0), calls)
    }

    @Test fun exactlyAPage() = runBlocking {
        val calls = mutableListOf<Int>()
        assertEquals(2000, collectPages(ITEMS_PAGE, server(2000, calls)).items.size)
        assertEquals(listOf(0), calls) // the total says it is all there
    }

    // The list changes right after the first page (a scan adds or removes an episode):
    // offsets shift, so one would be skipped or come twice. Pages of 3 over 7.
    private fun changing(before: List<Item>, after: List<Item>): suspend (Int) -> ItemsResponse {
        var list = before
        var calls = 0
        return { offset ->
            val r = ItemsResponse(list.drop(offset).take(3), list.size)
            if (++calls == 1) list = after
            r
        }
    }
    private fun eps(vararg n: Int) = n.map { Item(id = "e$it", type = "episode", title = "E$it") }

    @Test fun removedBeforeTheBoundaryIsNotASkip() = runBlocking {
        val r = collectPages(3, changing(eps(1, 2, 3, 4, 5, 6, 7), eps(1, 3, 4, 5, 6, 7)))
        assertEquals(listOf("e1", "e3", "e4", "e5", "e6", "e7"), r.items.map { it.id })
    }

    @Test fun addedBeforeTheBoundaryIsNotADuplicate() = runBlocking {
        val r = collectPages(3, changing(eps(1, 2, 3, 4, 5, 6, 7), eps(0, 1, 2, 3, 4, 5, 6, 7)))
        assertEquals(listOf("e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7"), r.items.map { it.id })
    }
}
