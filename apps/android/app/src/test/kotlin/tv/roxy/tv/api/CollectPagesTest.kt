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
}
