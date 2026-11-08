# Writes shrink.zip, implode.zip and reduce.zip: each member compressed with
# one PKZIP 1.x method from text made up here. unzip -t checks shrink and
# implode; reduce has no independent checker on this machine.
import sys, os, random
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from zipw import write_zip
from encoders import shrink, implode, reduce_
from sample import sample
out = sys.argv[1]
small = sample(3000, 1); far = sample(9000, 3) + sample(500, 4) + sample(9000, 3)[:3000]
runs = b"A" * 700 + b"\x90" * 50 + b"AB" * 400 + bytes(range(256))
r = random.Random(7)
# Varied enough to fill the 8192-entry table more than once.
churn = bytes(r.choice(b"abcdefghijklmnopqrstuvwxyz \r\n\x01\x90\xdb") for _ in range(30000)) + sample(12000, 9)
events = []
sh = [("small.txt", small), ("runs.txt", runs), ("churn.txt", churn)]
write_zip(os.path.join(out, "shrink.zip"), [(n, 1, 0, shrink(d, events=events), d) for n, d in sh])
print("shrink partial clears:", sum(1 for e in events if e[0] == "clear"))
im = []
for d8 in (False, True):
    for lit in (False, True):
        flags = (2 if d8 else 0) | (4 if lit else 0)
        for name, d in [("small", small), ("far", far), ("runs", runs)]:
            im.append(("%s-%s-%s.txt" % ("8k" if d8 else "4k", "3t" if lit else "2t", name), 6, flags, implode(d, d8, lit), d))
write_zip(os.path.join(out, "implode.zip"), im)
re_ = []
for f in (1, 2, 3, 4):
    for name, d in [("small", small), ("runs", runs), ("far", far)]:
        re_.append(("factor%d-%s.txt" % (f, name), 1 + f, 0, reduce_(d, f), d))
write_zip(os.path.join(out, "reduce.zip"), re_)

# An art pack as PKZIP 1.x made them, for the BBS's own tests (ZipLab, the
# scripting API and the archive viewer).
diz = b"\x1b[1;36mOLD PACK\x1b[0m  - a test art pack\r\nmade up for ViSiON/3's tests\r\n"
art = (b"\x1b[0;1;31m\x03\x03\x03 \x1b[33mOLD \xdb\xdb\xb2\xb1\xb0 PACK \x1b[31m\x03\x03\x03\x1b[0m\r\n" * 30) + b"\x1a" + b"SAUCE00" + b"Old Pack Art".ljust(35) + b"someartist".ljust(20) + b"testgroup".ljust(20) + b"19920101" + bytes(38)  # SAUCE is 128 bytes
readme = b"This pack was made for tests.\r\nNothing in it is anyone's real art.\r\n" * 4
notes = sample(2000, 11)
write_zip(os.path.join(out, "oldpack.zip"), [
    ("FILE_ID.DIZ", 6, 4, implode(diz, False, True), diz),
    ("OLDPACK.ANS", 6, 6, implode(art, True, True), art),
    ("README", 1, 0, shrink(readme), readme),
    ("NOTES.TXT", 3, 0, reduce_(notes, 2), notes),
])
# Entries nothing here can read: bzip2 (method 12) and an encrypted one.
write_zip(os.path.join(out, "unreadable.zip"), [
    ("BZIP2.TXT", 12, 0, b"BZh91AY&SY not really", b"not really bzip2"),
    ("SECRET.TXT", 0, 1, b"\x00" * 12 + b"secret", b"secret"),
])
