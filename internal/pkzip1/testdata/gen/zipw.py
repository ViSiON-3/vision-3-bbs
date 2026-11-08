# Minimal ZIP writer for already-compressed member data with any method.
import struct, zlib

def write_zip(path, members):
    """members: list of (name, method, flags, compressed_bytes, plain_bytes)."""
    out = bytearray(); central = bytearray()
    for name, method, flags, comp, plain in members:
        n = name.encode('cp437'); crc = zlib.crc32(plain) & 0xffffffff
        off = len(out); ver = 10
        out += struct.pack('<IHHHHHIIIHH', 0x04034b50, ver, flags, method, 0x6000, 0x1621, crc, len(comp), len(plain), len(n), 0) + n + comp
        central += struct.pack('<IHHHHHHIIIHHHHHII', 0x02014b50, ver, ver, flags, method, 0x6000, 0x1621, crc, len(comp), len(plain), len(n), 0, 0, 0, 0, 0x20, off) + n
    cd = len(out); out += central
    out += struct.pack('<IHHHHIIH', 0x06054b50, 0, 0, len(members), len(members), len(central), cd, 0)
    open(path, 'wb').write(out)

class BitWriter:
    """LSB-first bit writer, as PKZIP's shrink, reduce and implode use."""
    def __init__(self): self.buf = bytearray(); self.acc = 0; self.n = 0
    def write(self, value, bits):
        for i in range(bits):
            self.acc |= ((value >> i) & 1) << self.n; self.n += 1
            if self.n == 8: self.buf.append(self.acc); self.acc = 0; self.n = 0
    def bytes(self):
        b = bytearray(self.buf)
        if self.n: b.append(self.acc)
        return bytes(b)
