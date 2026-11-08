# Test-only encoders for PKZIP 1.x methods (shrink 1, reduce 2-5, implode 6),
# written from PKWARE's APPNOTE, to make fixtures. Shrink and implode output
# is checked with Info-ZIP's unzip -t; reduce has no independent checker here.
import heapq
from zipw import BitWriter

# ---------- Shrink (method 1): LZW 9-13 bits, partial clearing ----------
class ShrinkTable:
    """The decoder's table, which the encoder runs in lockstep."""
    def __init__(self):
        self.parent = [-1] * 8192; self.suffix = [0] * 8192
        self.free = [False] * 257 + [True] * (8192 - 257)
        self.child = {}  # (code, byte) -> code, for valid codes only
        self.next_free = 257
    def first(self, c):
        while c >= 257: c = self.parent[c]
        return c
    def lowest_free(self):
        while self.next_free < 8192 and not self.free[self.next_free]: self.next_free += 1
        return self.next_free if self.next_free < 8192 else None
    def add(self, prev, ch):
        c = self.lowest_free()
        if c is None: return
        self.parent[c] = prev; self.suffix[c] = ch; self.free[c] = False
        self.child[(prev, ch)] = c
    def partial_clear(self):
        has_child = [False] * 8192
        for c in range(257, 8192):
            if not self.free[c] and self.parent[c] >= 257: has_child[self.parent[c]] = True
        for c in range(257, 8192):
            if not has_child[c]: self.free[c] = True
        self.child = {(self.parent[c], self.suffix[c]): c for c in range(257, 8192) if not self.free[c]}
        self.next_free = 257
    def valid_chain(self, c):
        while c >= 257:
            if self.free[c]: return False
            c = self.parent[c]
        return True

def shrink(data, events=None):
    """Clears when the table is full, as PKZIP does. (Info-ZIP's unzip
    reads earlier clears differently from APPNOTE, so none are made.)"""
    t = ShrinkTable(); w = BitWriter(); size = 9
    w.write(data[0], size); prev = data[0]; pos = 1
    while pos < len(data):
        if t.lowest_free() is None:
            w.write(256, size); w.write(2, size); t.partial_clear()
            if events is not None: events.append(("clear", pos, prev))
        # Longest match through the children of valid codes.
        best = data[pos]; blen = 1
        while pos + blen < len(data):
            c = t.child.get((best, data[pos + blen]))
            if c is None or not t.valid_chain(c): break
            best = c; blen += 1
        while best >= (1 << size):
            w.write(256, size); w.write(1, size); size += 1
        w.write(best, size)
        t.add(prev, t.first(best))
        prev = best; pos += blen
    return w.bytes()

# ---------- Shannon-Fano trees for implode ----------
def huffman_lengths(freqs, maxbits=16):
    f = [x + 1 for x in freqs]
    while True:
        heap = [(x, i, (i,)) for i, x in enumerate(f)]; heapq.heapify(heap); depth = [0] * len(f); k = len(f)
        while len(heap) > 1:
            a = heapq.heappop(heap); b = heapq.heappop(heap)
            for s in a[2] + b[2]: depth[s] += 1
            heapq.heappush(heap, (a[0] + b[0], k, a[2] + b[2])); k += 1
        if max(depth) <= maxbits: return depth
        f = [x // 2 + 1 for x in f]

def sf_codes(lengths):
    """APPNOTE: codes from bit lengths, bit-reversed for LSB-first output."""
    order = sorted(range(len(lengths)), key=lambda i: (lengths[i], i))
    codes = [0] * len(lengths); code = 0; inc = 0; last = 0
    for i in reversed(order):
        code += inc
        if lengths[i] != last:
            last = lengths[i]; inc = 1 << (16 - last)
        codes[i] = code
    rev = []
    for c in codes:
        r = 0
        for b in range(16):
            if c & (1 << b): r |= 1 << (15 - b)
        rev.append(r)
    return rev

def write_tree(w, lengths):
    groups = []
    for L in lengths:
        if groups and groups[-1][0] == L and groups[-1][1] < 16: groups[-1][1] += 1
        else: groups.append([L, 1])
    w.write(len(groups) - 1, 8)
    for L, n in groups: w.write(((n - 1) << 4) | (L - 1), 8)

def lz(data, window, minm, maxm):
    """Greedy LZ77 with a hash of 3-byte (or 2-byte) heads, for speed."""
    heads = {}; toks = []; pos = 0; k = minm
    while pos < len(data):
        bl, bd = 0, 0
        key = bytes(data[pos:pos + k])
        for cand in reversed(heads.get(key, [])[-256:]):
            d = pos - cand
            if d > window: break
            L = 0
            while L < maxm and pos + L < len(data) and data[pos + L - d] == data[pos + L]: L += 1
            if L > bl: bl, bd = L, d
            if bl == maxm: break
        step = bl if bl >= minm else 1
        for i in range(pos, min(pos + step, len(data))):
            heads.setdefault(bytes(data[i:i + k]), []).append(i)
        if bl >= minm: toks.append((bl, bd))
        else: toks.append(data[pos])
        pos += step
    return toks

def implode(data, dict8k, lit_tree):
    minm = 3 if lit_tree else 2; lowbits = 7 if dict8k else 6; window = 8192 if dict8k else 4096
    toks = lz(data, window, minm, minm + 63 + 255)
    lf = [0] * 256; nf = [0] * 64; df = [0] * 64
    for t in toks:
        if isinstance(t, int): lf[t] += 1
        else: nf[min(t[0] - minm, 63)] += 1; df[(t[1] - 1) >> lowbits] += 1
    w = BitWriter()
    ll = huffman_lengths(lf) if lit_tree else None; nl = huffman_lengths(nf); dl = huffman_lengths(df)
    if lit_tree: write_tree(w, ll)
    write_tree(w, nl); write_tree(w, dl)
    lc = sf_codes(ll) if lit_tree else None; nc = sf_codes(nl); dc = sf_codes(dl)
    for t in toks:
        if isinstance(t, int):
            w.write(1, 1)
            if lit_tree: w.write(lc[t], ll[t])
            else: w.write(t, 8)
        else:
            L, d = t; w.write(0, 1)
            w.write((d - 1) & ((1 << lowbits) - 1), lowbits)
            hi = (d - 1) >> lowbits; w.write(dc[hi], dl[hi])
            s = min(L - minm, 63); w.write(nc[s], nl[s])
            if s == 63: w.write(L - minm - 63, 8)
    return w.bytes()

# ---------- Reduce (methods 2-5) ----------
DLE = 144
def B(n):
    return 0 if n == 0 else max(1, (n - 1).bit_length())

def reduce_(data, factor):
    mask = 0xFF >> factor; maxdist = 256 << factor
    toks = lz(data, maxdist, 3, 3 + mask + 255)
    cs = []; pos = 0
    def lit(b):
        return [DLE, 0] if b == DLE else [b]
    for t in toks:
        if isinstance(t, int):
            cs += lit(t); pos += 1; continue
        L, d = t; lp = L - 3; v = ((d - 1) >> 8) << (8 - factor)
        if lp == 0 and v == 0:
            # V = 0 means a literal DLE, so this match can't be coded:
            # send its three bytes as literals.
            for b in data[pos:pos + 3]: cs += lit(b)
        elif lp < mask: cs += [DLE, v | lp, (d - 1) & 0xFF]
        else: cs += [DLE, v | mask, lp - mask, (d - 1) & 0xFF]
        pos += L
    pairs = {}; last = 0
    for c in cs:
        pairs.setdefault(last, {}); pairs[last][c] = pairs[last].get(c, 0) + 1; last = c
    sets = {}
    for j in range(256):
        cand = sorted(pairs.get(j, {}).items(), key=lambda kv: -kv[1])
        n = 0 if j % 7 == 3 else min(len(cand), [32, 5, 1, 17][j % 4])
        sets[j] = [k for k, _ in cand[:n]]
    w = BitWriter()
    for j in range(255, -1, -1):
        w.write(len(sets[j]), 6)
        for b in sets[j]: w.write(b, 8)
    last = 0
    for c in cs:
        s = sets[last]
        if not s: w.write(c, 8)
        elif c in s: w.write(0, 1); w.write(s.index(c), B(len(s)))
        else: w.write(1, 1); w.write(c, 8)
        last = c
    return w.bytes()
