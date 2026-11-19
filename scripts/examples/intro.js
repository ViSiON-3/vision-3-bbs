// intro.js — a rotating ASCII wireframe cube entrance, a VPL port of the
// Vision/3 connection intro's "cube" style. The cube turns in chrome
// tones, a title sits above it and a loading line travels below. It grows
// in at the start and, at the end, the whole scene contracts toward the
// centre of the screen and vanishes into nothing. Space or Enter skips.
//
// Every frame is built as one map of cells and diffed against the last,
// so a turning wireframe is a few dozen changed cells per frame — light
// enough for a real connection, unlike a full-screen pixel effect.
//
// Configure in doors.json:
//   { "code": "INTRO", "name": "Intro", "type": "v3_script",
//     "script": "intro.js", "working_directory": "scripts/examples" }
// Run it from a menu, or put {"command":"DOOR:INTRO"} first in the logon
// sequence. The optional first arg is the length in seconds (default 16).

var W = v3.console.width;
var H = v3.console.height;
var DURATION = parseInt(v3.args[0], 10) || 16;
var FRAME_MS = 80;

// The cube's box, centred, with room for a title above and a line below.
var CW = Math.min(23, W);
var CH = Math.min(14, H - 4);
var CLEFT = ((W - CW) / 2) | 0;    // 0-based screen column of the box
var CTOP = (((H - CH) / 2) | 0) + 1;

var EDGES = [[0, 1], [2, 3], [4, 5], [6, 7], [0, 2], [1, 3], [4, 6], [5, 7],
    [0, 4], [1, 5], [2, 6], [3, 7]];
// A magenta cube ramp (truecolor): dark violet-magenta shadows through
// magenta and pink to an icy near-white highlight.
var SHADE = [
    "\x1b[38;2;40;10;52m",
    "\x1b[38;2;92;18;104m",
    "\x1b[38;2;158;28;166m",
    "\x1b[38;2;212;48;196m",
    "\x1b[38;2;240;130;224m",
    "\x1b[38;2;255;224;250m"
];
var LOADING = "loading bbs .... please wait ....";

// A quiet star field behind the whole scene: dim dots that hold still, a
// scattered few that twinkle now and then. Positions are fixed at load (a
// seeded LCG) so nothing jumps between frames. A brightness ramp runs from
// a deep blue-grey rest up through grey to a white peak and back.
var STAR_RAMP = [
    "\x1b[38;2;46;46;54m",
    "\x1b[38;2;84;84;96m",
    "\x1b[38;2;130;130;145m",
    "\x1b[38;2;180;180;195m",
    "\x1b[38;2;220;220;230m",
    "\x1b[38;2;255;255;255m"
];
var STARS = (function () {
    var list = [];
    var seed = 1337;
    function rnd() {
        seed = (seed * 1103515245 + 12345) & 0x7fffffff;
        return seed / 0x7fffffff;
    }
    var count = Math.max(8, (W * H / 55) | 0);
    for (var i = 0; i < count; i++) {
        var depth = rnd(); // 0 = far, 1 = near
        list.push({
            row: 1 + ((rnd() * H) | 0),
            col: 1 + rnd() * W,          // float, for smooth drift
            speed: 0.2 + depth * 1.0,    // nearer stars drift faster (parallax)
            rest: depth < 0.4 ? 0 : depth < 0.8 ? 1 : 2, // nearer a touch brighter
            period: 10 + rnd() * 12,     // seconds between this star's twinkles
            phase: rnd(),                // 0..1 offset into its cycle
            twinkles: rnd() < 0.45
        });
    }
    return list;
})();

function clamp(v, a, b) {
    return v < a ? a : (v > b ? b : v);
}

function smooth(p) {
    p = clamp(p, 0, 1);
    return p * p * (3 - 2 * p);
}

// A cell map is keyed by row*1000+col (both 1-based); the value is a
// pipe-coded single glyph. These helpers build and combine the layers.
function merge() {
    var out = {};
    for (var a = 0; a < arguments.length; a++) {
        var m = arguments[a];
        for (var k in m) {
            out[k] = m[k];
        }
    }
    return out;
}

// shade maps a turning wireframe point to a chrome level: a base glow, a
// near-depth lift, soft diffuse light and a narrow specular highlight.
function shade(x, y, z, grow) {
    var len = Math.sqrt(x * x + y * y + z * z);
    if (len === 0) {
        return 0;
    }
    var nx = x / len, ny = y / len, nz = z / len;
    var near = clamp((1.75 - z) / 3.5, 0, 1);
    var diffuse = Math.max(0, -0.35 * nx - 0.5 * ny - 0.792 * nz);
    var spec = Math.pow(Math.max(0, -0.185 * nx - 0.264 * ny - 0.947 * nz), 18);
    var b = (0.16 + 0.48 * near + 0.2 * diffuse + 0.3 * spec) * (0.3 + 0.7 * grow);
    return clamp(Math.floor(b * SHADE.length), 0, SHADE.length - 1);
}

// cubeCells returns the wireframe at time t. Two depth samples per cell
// give sub-row detail: a mark in the top half is ', in the bottom half .,
// both is :.
function cubeCells(t, grow) {
    var sx = Math.sin(t * 0.43 + 0.3), cx = Math.cos(t * 0.43 + 0.3);
    var sy = Math.sin(t * 0.61 + 0.5), cy = Math.cos(t * 0.61 + 0.5);
    var sz = Math.sin(t * 0.19 - 0.2), cz = Math.cos(t * 0.19 - 0.2);
    var scale = Math.min(CW / 2, CH) * 0.27 * grow;
    var verts = [], world = [];
    for (var i = 0; i < 8; i++) {
        var x = (i & 1) * 2 - 1, y = ((i >> 1) & 1) * 2 - 1, z = ((i >> 2) & 1) * 2 - 1;
        var ny = y * cx - z * sx, nz = y * sx + z * cx;
        y = ny; z = nz;
        var nx = x * cy + z * sy;
        nz = -x * sy + z * cy;
        x = nx; z = nz;
        nx = x * cz - y * sz; ny = x * sz + y * cz;
        x = nx; y = ny;
        world[i] = { x: x, y: y, z: z };
        var d = 4 / (4.5 + z);
        verts[i] = {
            x: CLEFT + (CW - 1) / 2 + x * d * scale * 2,
            y: CTOP - 1 + (CH - 1) / 2 + y * d * scale,
            z: z
        };
    }
    var sub = CW * CH * 2;
    var zbuf = [], cbuf = [];
    for (var k = 0; k < sub; k++) {
        zbuf[k] = Infinity;
        cbuf[k] = 0;
    }
    function put(px, py, z, col) {
        var lx = px - CLEFT, ly = py - (CTOP - 1) * 2;
        if (lx < 0 || lx >= CW || ly < 0 || ly >= CH * 2) {
            return;
        }
        var at = (((ly >> 1) * CW) + lx) * 2 + (ly & 1);
        if (z <= zbuf[at]) {
            zbuf[at] = z;
            cbuf[at] = col;
        }
    }
    for (var e = 0; e < EDGES.length; e++) {
        var a = verts[EDGES[e][0]], b = verts[EDGES[e][1]];
        var ax = Math.round(a.x), ay = Math.round(a.y * 2);
        var bx = Math.round(b.x), by = Math.round(b.y * 2);
        var steps = Math.max(1, Math.max(Math.abs(bx - ax), Math.abs(by - ay)));
        var wa = world[EDGES[e][0]], wb = world[EDGES[e][1]];
        for (var s = 0; s <= steps; s++) {
            var u = s / steps;
            var denom = (1 - u) / (4.5 + a.z) + u / (4.5 + b.z);
            var zz = 1 / denom - 4.5;
            var vv = (u / (4.5 + b.z)) / denom;
            put(ax + Math.round(u * (bx - ax)), ay + Math.round(u * (by - ay)), zz,
                shade(wa.x + (wb.x - wa.x) * vv, wa.y + (wb.y - wa.y) * vv, zz, grow));
        }
    }
    var cells = {};
    for (var row = 0; row < CH; row++) {
        for (var col = 0; col < CW; col++) {
            var base = ((row * CW) + col) * 2;
            var up = zbuf[base], lo = zbuf[base + 1];
            if (up === Infinity && lo === Infinity) {
                continue;
            }
            var ch, ci;
            if (up === Infinity) {
                ch = "."; ci = cbuf[base + 1];
            } else if (lo === Infinity) {
                ch = "'"; ci = cbuf[base];
            } else {
                ch = ":"; ci = lo < up ? cbuf[base + 1] : cbuf[base];
            }
            cells[(CTOP + row) * 1000 + (CLEFT + col + 1)] = SHADE[ci] + ch;
        }
    }
    return cells;
}

// starfield returns the star layer at time t. Each star rests at a dim
// grey; a twinkler, once per its period, ramps smoothly up through grey to
// white and back down over a short window, then holds still again.
function starfield(t) {
    var cells = {};
    var top = STAR_RAMP.length - 1;
    for (var i = 0; i < STARS.length; i++) {
        var st = STARS[i];
        var lvl = st.rest;
        if (st.twinkles) {
            var frac = (t / st.period + st.phase) % 1;
            if (frac < 0.12) {
                var up = Math.round(Math.sin((frac / 0.12) * Math.PI) * top);
                if (up > lvl) {
                    lvl = up;
                }
            }
        }
        // soft parallax: drift left, nearer stars faster, wrapping around.
        var c0 = (((st.col - 1 - st.speed * t) % W) + W) % W;
        cells[st.row * 1000 + ((c0 | 0) + 1)] = STAR_RAMP[lvl] + ".";
    }
    return cells;
}

function titleCells() {
    var name = v3.session.bbs.name;
    var row = Math.max(1, CTOP - 2);
    var left = ((W - name.length) / 2 | 0) + 1;
    var cells = {};
    for (var i = 0; i < name.length; i++) {
        var ch = name.charAt(i);
        if (ch !== " ") {
            // the i's drop to low-intensity cyan
            var col = ch === "i" || ch === "I" ? "|03" : "|11";
            cells[row * 1000 + (left + i)] = col + ch;
        }
    }
    return cells;
}

// The Go loading line's neutral grey fade (palette indices 1-4).
var GRAY = ["", "\x1b[38;2;45;45;45m", "\x1b[38;2;75;75;75m",
    "\x1b[38;2;110;110;110m", "\x1b[38;2;150;150;150m"];

// loadingCells is the Go drawCubeLoading: each character repeats a fade-up
// as a highlight travels left to right, cooling to dark grey behind it.
function loadingCells(t) {
    var cells = {};
    var row = CTOP + CH;
    if (row >= H) {
        return cells;
    }
    var left = ((W - LOADING.length) / 2 | 0) + 1;
    for (var i = 0; i < LOADING.length; i++) {
        var ch = LOADING.charAt(i);
        var age = t - (0.5 + i * 0.09);
        if (ch === " " || age < 0) {
            continue;
        }
        var phase = age % 4;
        var color = 1;
        if (phase < 0.54) {
            color = 1 + Math.min(2, (phase / 0.18) | 0);
        } else if (phase < 0.72) {
            color = 4;
        } else if (phase < 1.09) {
            color = 3;
        } else if (phase < 1.48) {
            color = 2;
        }
        cells[row * 1000 + (left + i)] = GRAY[color] + ch;
    }
    return cells;
}

// shrinkScene contracts a cell map toward the centre of the screen by
// scale, so the whole composition collapses to a speck and vanishes. Near
// the end the marks fade to grey dots.
function shrinkScene(cells, scale) {
    var out = {};
    if (scale <= 0.03) {
        return out;
    }
    var cx = (W + 1) / 2, cy = (H + 1) / 2;
    for (var key in cells) {
        var col = key % 1000, row = (key / 1000) | 0;
        var nc = Math.round(cx + (col - cx) * scale);
        var nr = Math.round(cy + (row - cy) * scale);
        if (nc < 1 || nc > W || nr < 1 || nr > H) {
            continue;
        }
        var val = cells[key];
        if (scale < 0.25) {
            val = "|08.";
        }
        out[nr * 1000 + nc] = val;
    }
    return out;
}

function growIn(t) {
    return t < 2.5 ? smooth(t / 2.5) : 1;
}

function center(text, row, colour) {
    v3.console.gotoxy(((W - text.length) / 2 | 0) + 1, row);
    v3.console.print(colour + text);
}

function run() {
    // Match the Go intro's entry reset exactly: full SGR reset, clear,
    // home, hide cursor.
    v3.console.print("\x1b[0m\x1b[2J\x1b[H\x1b[?25l");
    center("[ space skips ]", H, "|08");
    var start = Date.now();
    var prev = {};
    for (;;) {
        var t = (Date.now() - start) / 1000;
        if (t >= DURATION) {
            break;
        }
        var scene = merge(starfield(t), cubeCells(t, growIn(t)), titleCells(), loadingCells(t));
        if (t > DURATION - 2) {
            scene = shrinkScene(scene, 1 - smooth((t - (DURATION - 2)) / 2));
        }
        var key;
        for (key in scene) {
            if (scene[key] !== prev[key]) {
                v3.console.gotoxy(key % 1000, (key / 1000) | 0);
                v3.console.print(scene[key]);
            }
        }
        for (key in prev) {
            if (scene[key] === undefined) {
                v3.console.gotoxy(key % 1000, (key / 1000) | 0);
                v3.console.print(" ");
            }
        }
        prev = scene;
        v3.console.gotoxy(W, H);
        var k = v3.console.getkey(FRAME_MS);
        if (k === " " || k === "\r") {
            break;
        }
    }
    // Match the Go intro's exit cleanup exactly: full SGR reset, show
    // cursor, clear, home — so nothing bleeds into the screens after.
    v3.console.print("\x1b[0m\x1b[?25h\x1b[2J\x1b[H");
}

if (W < 40 || H < 16) {
    v3.console.println("|07Your terminal is too small for the intro.");
    v3.console.pause();
} else {
    run();
}
