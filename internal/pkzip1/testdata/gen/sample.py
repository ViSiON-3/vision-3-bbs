import random
def sample(n, seed):
    r = random.Random(seed)
    words = [b"ViSiON", b"BBS", b"ansi", b"\x1b[1;31m", b"\x1b[0m", b"\xdb\xdb\xb2\xb1\xb0", b"\x03\x01", b"sysop", b"node", b"\r\n", b"the", b"of", b"\x90", b"artpack"]
    out = bytearray()
    while len(out) < n:
        if r.random() < 0.05: out += bytes(r.randrange(256) for _ in range(r.randrange(1, 12)))
        elif r.random() < 0.1 and len(out) > 300: s = r.randrange(len(out) - 200); out += out[s:s + r.randrange(20, 200)]
        else: out += r.choice(words) + b" "
    return bytes(out[:n])
