package tv.roxy.tv.api

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test

class CollectPagesTest {
    private fun eps(vararg n: Int) = n.map { Item(id = "e$it", type = "episode", title = "E$it") }
    private fun many(n: Int) = (1..n).map { Item(id = "e$it", type = "episode", title = "E$it") }

    // A fake /v1/items: sends at most [cap] whatever is asked (the server's own cap).
    private fun server(all: List<Item>, cap: Int, calls: MutableList<Int>): suspend (Int) -> ItemsResponse = { offset ->
        calls += offset
        ItemsResponse(all.drop(offset).take(cap), all.size, 1)
    }

    // A long list comes back whole, not its first page.
    @Test fun readsEveryPage() = runBlocking {
        val calls = mutableListOf<Int>()
        val r = collectPages(ITEMS_PAGE, server(many(4500), 2000, calls))
        assertEquals(4500, r.items.size)
        assertEquals("e4500", r.items.last().id)
        assertEquals(listOf(0, 2000, 4000), calls)
    }

    // Up to the server's cap: one request, one consistent read.
    @Test fun oneRequestWhenItFits() = runBlocking {
        val calls = mutableListOf<Int>()
        assertEquals(1235, collectPages(ITEMS_PAGE, server(many(1235), 20000, calls)).items.size)
        assertEquals(listOf(0), calls)
    }

    // The server sends fewer than asked (5000 with sources): a short page is not the end;
    // the total is. Stopping at the first short page lost everything after it.
    @Test fun shortPageIsNotTheEnd() = runBlocking {
        val calls = mutableListOf<Int>()
        assertEquals(12000, collectPages(ITEMS_PAGE, server(many(12000), 5000, calls)).items.size)
        assertEquals(listOf(0, 5000, 10000), calls)
    }

    // The list changes right after the first page (a scan adds or removes an episode):
    // offsets shift, so one would be skipped or come twice. Pages of 3 over 7.
    private fun changing(before: List<Item>, after: List<Item>): suspend (Int) -> ItemsResponse {
        var list = before
        var version = 1L
        var calls = 0
        return { offset ->
            val r = ItemsResponse(list.drop(offset).take(3), list.size, version)
            if (++calls == 1) { list = after; version++ }
            r
        }
    }

    @Test fun removedBeforeTheBoundaryIsNotASkip() = runBlocking {
        val r = collectPages(3, changing(eps(1, 2, 3, 4, 5, 6, 7), eps(1, 3, 4, 5, 6, 7)))
        assertEquals(listOf("e1", "e3", "e4", "e5", "e6", "e7"), r.items.map { it.id })
    }

    @Test fun addedBeforeTheBoundaryIsNotADuplicate() = runBlocking {
        val r = collectPages(3, changing(eps(1, 2, 3, 4, 5, 6, 7), eps(0, 1, 2, 3, 4, 5, 6, 7)))
        assertEquals(listOf("e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7"), r.items.map { it.id })
    }

    // One removed and another added between the pages: the total stays 7, the version moves.
    @Test fun replacementWithTheSameTotalIsReadAgain() = runBlocking {
        val r = collectPages(3, changing(eps(1, 2, 3, 4, 5, 6, 7), eps(1, 3, 4, 5, 6, 7, 8)))
        assertEquals(listOf("e1", "e3", "e4", "e5", "e6", "e7", "e8"), r.items.map { it.id })
    }

    // A busy scan moving the counter between every two requests (worst case, past the cap):
    // three tries of two requests each, never a duplicate.
    @Test fun constantChurnIsBounded() = runBlocking {
        var requests = 0
        var version = 0L
        val all = many(7)
        val r = collectPages(3) { offset -> requests++; ItemsResponse(all.drop(offset).take(3), all.size, version++) }
        assertEquals(6, requests)
        assertEquals(r.items.size, r.items.distinctBy { it.id }.size)
        assertEquals(false, r.complete) // never read from one state: said, not hidden
    }

    // The server claims 10 and sends an empty page after 4: no loop, and the list says it
    // is incomplete (with the server's total) instead of passing for whole.
    @Test fun emptyPageShortOfTheTotalIsReported() = runBlocking {
        var requests = 0
        val all = many(10)
        val r = collectPages(ITEMS_PAGE) { offset -> requests++; ItemsResponse(if (offset < 4) all.subList(offset, 4) else emptyList(), 10, 1) }
        assertEquals(4, r.items.size)
        assertEquals(false, r.complete)
        assertEquals(10, r.total)
        assertEquals(2, requests)
    }

    @Test fun wholeReadIsComplete() = runBlocking {
        assertEquals(true, collectPages(ITEMS_PAGE, server(many(5), 2000, mutableListOf())).complete)
    }
}
