This is a very vibecoded agent for analyzing and comparing the outputs from
Golang's performance profiler, [pprof](https://pkg.go.dev/runtime/pprof).
It might (that's the operative word!) help you with debugging/performance
analysis across multiple implementations of a bit of code.

Although pprof is relatively structured, over the past few months I found that
Haiku (obviously), Sonnet, and then even Opus 4.6 / 4.7 struggled with
_really_ digging into pprof output and the implications of specific results.
I have not done formal evals (obviously),
but I've found (anecdotally) that the tool use allows Sonnet 4.6 to perform
analysis "at least as good" as just dumping a profile into the Claude UI
w. Opus 4.7.

Here's a toy-ish conversation with the agent that you can ~replicate by
cloning this repo, calling `./cmd/loop` to start the agent,

```text
> Take a look at the profile in ./fixtures/pprof/inv.query.heap.pprof,
what's hot?

[REDACTED FOR README] A bunch of tool calls, log lines, && yapping...

## Heap Profile: `inv.query.heap.pprof`

This is a heap profile for what looks like an inverted index implementation
(think Lucene-style posting-list intersection).
Total allocated: **~823 MB**. Nearly nothing is *retained* (inuse_space ≈ 2
MB, all from `runtime.allocm` for thread stacks),
so this is a **throughput / allocation-pressure story**, not a leak.

---

### Alloc Space Hotspots (cumulative)
   # | Function | Alloc (bytes) | % Total |
 |---|---|---|---|
 | 🔥 | `main.(*InvIndex).Intersection` | 611 MB | **74.3%** |
 | 2 | `main.ingest` → `AddDocument` + `bufio.Scanner.Text` | 95 MB | 11.5% |
 | 3 | `main.makeQueries` | 71 MB | 8.6% |
 | 4 | `main.init.func8` → `NewInvIndex` | 34 MB | 4.1% |
 | 5 | `bufio.(*Scanner).Text` | 33 MB | 3.9% |

---

### Key Findings

**1. `(*InvIndex).Intersection` is the dominant allocator at 74.3%.**
64 sample stacks all terminate here, with individual samples ranging from
~20–84 MB. The flat cost equals the cumulative cost — there are no callees
showing up — which means the allocations are happening *directly inside*
`Intersection`, not in something it calls. Classic symptom: the intersection
is **building result sets by allocating new slices on each call** rather
than writing into a pre-allocated buffer or returning an iterator. For
posting-list intersection (à la SvS or DAAT), the standard fix is:

- Use a caller-provided scratch buffer (`[]docID`) and return a subslice.
- Or better: return a lazy iterator/cursor over the posting lists and avoid
  materializing the result entirely.

**2. `makeQueries` at 8.6% flat.**
Query construction is also allocating heavily — likely building `[]string`
or `[][]string` query term lists. If queries follow a predictable structure,
pooling (`sync.Pool`) or pre-sizing slices would help.

**3. `AddDocument` / `bufio.Scanner.Text` at ~11% combined during
ingest.**
`Scanner.Text` allocates a new `string` on every call (it copies the
buffer). If ingest is hot, consider `Scanner.Bytes()` + intern/dedup into
the index rather than converting to `string` per line.

**4. `NewInvIndex` at 4.1%.**
One-time initialization cost — probably allocating the map/posting-list
structure. Less actionable unless you're reinitializing frequently.

---
### Bottom Line

**The smoking gun is `Intersection`.** It's allocating ~611 MB in what
should be a read-only query operation. This will hammer the GC — every
intersection materializes a new `[]T`, contributing to allocation pressure
and stop-the-world (or concurrent GC) overhead proportional to object count.

**Recommended next step:** Refactor `Intersection` to accept a `dst
[]DocID` scratch buffer (pool it via `sync.Pool`), or restructure it as a
merge-style cursor that doesn't need to materialize the result at all.
The latter is the approach taken by systems like Lucene's
`ConjunctionScorer` and is essentially free in allocation terms.

---
And here's an example conversation with the agent that _actually_
improved my quality of life...

```text
> I have a bunch of implementations of an inverted index, split by build
and query phase in .../index/prof, each impl has a file named
`${impl}.${phase}.(cpu|heap).pprof`. Upload the inv and inv-nopool
implementations' cpu and heap profiles, analyze them individually, tell me
what's hot in each impl, then give me a summary of how they all compare.
Is the addition of sync.Pool worth it?

[REDACTED FOR README] A bunch of tool calls + thinking, no additional
prompt from me...
[REDACTED FOR README] A bunch of intermediate graphs and tables...

## Verdict: Is `sync.Pool` Worth It?

**Yes, but with an important caveat about where the benefit actually
lives.**
 | Phase | Metric | nopool | inv (pool) | Delta |
 |---|---|---|---|---|
 | Build | CPU | 370ms | 320ms | **−13.5%** ✅ |
 | Build | Heap alloc | 267.6 MB | 150.5 MB | **−43.7%** ✅ |
 | Query | CPU | 2,620ms | 2,650ms | **+1.1%** ≈ wash |
 | Query | Heap alloc | 978.3 MB | 840.3 MB | **−14.1%** ✅ |

The pool's win is primarily in **allocation reduction** — 118.5 MB
eliminated from `tokenizeHashes` across both phases. At build time this
translates to a clean CPU win too (less GC, less memclr). At query time
the CPU is a wash: the pool is trading GC collection work for explicit
`memclrNoHeapPointers` zeroing on reuse, and those roughly cancel out. The
GC marks (`scanobject`, `greyobject`) do improve, which matters for latency
tail percentiles and GC pause frequency even if mean CPU is flat.

**The real story:** the bottleneck in both impls is `mergeU32` inside
`Intersection` (~83–93% of query CPU). The pool doesn't help that at all
— it's pure algorithmic work on the posting lists. If you want to push
query performance further, the lever is in `mergeU32` itself: look at
SIMD-friendly intersection (à la Lemire's SIMD bitset intersection),
roaring bitmaps for compressed posting lists, or skipping blocks when the
lists are heavily skewed in length. The pool is a sensible hygiene
improvement but it's not going to move the needle on your p99 query
latency