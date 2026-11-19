package ansi

// AudioLibsndfileQuery asks a terminal that implements SyncTERM's audio
// sequences whether libsndfile, which loads its sound files, is present. It
// replies CSI = 7 ; 100 ; 1 n when it is and CSI = 7 ; 100 ; 0 n when it is
// not (syncterm.bbsdev.net/cterm.html, "APC SyncTERM:Q; Ps ST"). Terminals
// that do not know it send nothing.
const AudioLibsndfileQuery = "\x1b_SyncTERM:Q;libsndfile\x1b\\"

// AudioWAVU8Query asks whether the terminal plays WAV with 8-bit unsigned
// PCM (libsndfile major format 1, subtype 5), the board's audio format. It
// replies CSI = 7 ; 101 ; 1 ; 5 ; 1 n when it does and the same ending in 0
// when it does not. The values are sent in decimal, which IcyTERM requires.
const AudioWAVU8Query = "\x1b_SyncTERM:Q;libsndfileFormat;1;5\x1b\\"

// AudioQueries is AudioLibsndfileQuery and AudioWAVU8Query with autowrap
// turned off around them. A terminal that does not parse APC strings, such
// as rxvt-unicode, draws their bodies as text; with autowrap off the text
// stays on the cursor's row, which the probe erases when it ends, instead
// of wrapping onto rows it does not erase. Autowrap is on again before the
// next byte.
const AudioQueries = "\x1b[?7l" + AudioLibsndfileQuery + AudioWAVU8Query + "\x1b[?7h"

// AudioLibsndfileYes and AudioWAVU8Yes are the replies that say yes to
// AudioLibsndfileQuery and AudioWAVU8Query.
const (
	AudioLibsndfileYes = "\x1b[=7;100;1n"
	AudioWAVU8Yes      = "\x1b[=7;101;1;5;1n"
)

// AudioLibsndfileNo and AudioWAVU8No are the replies that say no.
const (
	AudioLibsndfileNo = "\x1b[=7;100;0n"
	AudioWAVU8No      = "\x1b[=7;101;1;5;0n"
)

// IsAudioLibsndfileReply reports whether reply is a whole answer to
// AudioLibsndfileQuery, yes or no.
func IsAudioLibsndfileReply(reply []byte) bool {
	return string(reply) == AudioLibsndfileYes || string(reply) == AudioLibsndfileNo
}

// IsAudioWAVU8Reply reports whether reply is a whole answer to
// AudioWAVU8Query, yes or no.
func IsAudioWAVU8Reply(reply []byte) bool {
	return string(reply) == AudioWAVU8Yes || string(reply) == AudioWAVU8No
}
