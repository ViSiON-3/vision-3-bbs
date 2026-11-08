# Test archive generator

`shrink.zip`, `implode.zip` and `reduce.zip` in `../` were made with:

```
python3 gen.py ..
unzip -t ../shrink.zip ../implode.zip   # Info-ZIP's unzip, as an independent check
```

`encoders.py` holds test-only encoders for PKZIP 1.x's shrink (method 1),
reduce (2 to 5) and implode (6), written from PKWARE's APPNOTE.TXT. All the
text is made up by `sample.py` and `gen.py`; no outside files are used.

Info-ZIP's `unzip -t` passes `shrink.zip` and `implode.zip`: a CRC match
there means the archive decodes as Info-ZIP reads the format, independently
of this package. Info-ZIP doesn't read reduced members, so `reduce.zip` is
checked only by this package's decoder against the encoder here.
