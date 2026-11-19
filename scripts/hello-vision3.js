// hello-vision3.js: the ViSiON/3 Hello door.
//
// Greets the caller by handle. On a terminal that draws sixel it is one
// 640x368 canvas: the logo dropped onto the screen like hot metal, the
// handle in chrome lettering that fades up and away, then a sine scroller
// with the caller's details, copper bars, a starfield and forge sparks. Anywhere else it is 16-colour
// ANSI: the logo and the handle in block letters with colour cycling, and a
// wave scroller with the same details. Any key returns to the menu.
//
// doors.json:
//   { "code": "HELLO", "type": "v3_script", "script": "hello-vision3.js",
//     "working_directory": "scripts" }
//
// The picture needs scripts/data/hello-vision3-logo.png (the theme's logo
// with its background cut out) and scripts/data/hello-vision3-palette.png
// (one pixel of every colour in the ramps below and in the logo). Without
// them the door draws the ANSI version.
var w = v3.console.width, h = v3.console.height;
var me = v3.user || v3.session.user || {};
var handle = String(me.handle || 'caller');
var calls = Number(me.timesCalled) || 0;
var node = v3.session.node;
var board = String(v3.session.bbs.name || 'this board');
var months = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];

// The caller's previous call, or '' on a first call. lastLogin is this
// call, so the record's previousLogin is the one to show.
function lastCall() {
  var record = v3.users && v3.users.get ? v3.users.get(handle) : null;
  if (!record || !record.previousLogin) return '';
  var d = new Date(record.previousLogin * 1000);
  function two(n) { return (n < 10 ? '0' : '') + n; }
  return d.getDate() + ' ' + months[d.getMonth()] + ' ' + d.getFullYear() + ' AT ' + two(d.getHours()) + ':' + two(d.getMinutes());
}
var previous = lastCall();

// The scroller's text. marks has a '1' under every character of the
// caller's own details, which are drawn in steel; the rest is amber.
var message = '', marks = '';
function say(text, detail) {
  message += text;
  for (var i = 0; i < text.length; i++) marks += detail ? '1' : '0';
}
say('HELLO '); say(handle.toUpperCase(), true); say('!   WELCOME TO '); say(board.toUpperCase(), true);
say('   ...   THIS IS CALL NUMBER '); say(String(calls), true);
if (previous) { say('   ...   YOUR LAST CALL WAS '); say(previous, true); }
else say('   ...   THIS IS YOUR FIRST CALL');
say('   ...   YOU ARE ON NODE '); say(String(node), true);
say('   ...   VISION/3: SCRIPTED MENUS, SIXEL PICTURES, DOORS LIKE THIS ONE   ...   ANY KEY RETURNS TO THE MENU   ...      ');

// A 5x7 font. FONT_ROWS holds seven base-32 digits a glyph, one per row,
// bit 4 the leftmost pixel; the glyph after the last character is the box
// drawn for a character the font does not have.
var FONT_CHARS = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_|~";
var FONT_ROWS = '00000004444404aaa0000aavavaa4fke5u4pp248jjcik8lid4480000248884284222480level0044v4400000c48000v00000000cc11248ggehjlphe4c4444eeh1248vu11e11u26aiv22vgu11he68guhhev124888ehhehheehhf12c0cc0cc00cc0c48248g84200v0v008421248eh12404ehnlngfehhvhhhuhhuhhuehgggheuhhhhhuvgguggvvggugggehgnhhfhhhvhhhe44444e72222ichikokihggggggvhrllhhhhpljhhhehhhhheuhhugggehhhliduhhukihfgge11uv444444hhhhhhehhhhha4hhhllrhhha4ahhhha4444v1248gve88888egg84211e22222e4ah0000000000v4444444008l200vhhhhhv';
var glyphs = {};
function glyph(ch) {
  var rows = glyphs[ch];
  if (rows) return rows;
  var at = FONT_CHARS.indexOf(ch);
  if (at < 0) at = FONT_CHARS.length;
  rows = [];
  for (var r = 0; r < 7; r++) rows.push(parseInt(FONT_ROWS.charAt(at * 7 + r), 32));
  glyphs[ch] = rows;
  return rows;
}

// ---------------------------------------------------------------------------
// The picture.

// The canvas's metals, dark to light. The palette picture has exactly
// these colours, so a ramp step is drawn as itself.
var AMBER = [[0,0,0],[8,2,0],[16,4,0],[24,6,0],[32,7,0],[40,9,0],[48,11,0],[58,14,0],[70,18,0],[81,21,0],[93,25,0],[105,29,0],[116,32,0],[128,36,0],[140,43,0],[151,51,0],[163,58,0],[175,66,0],[186,73,0],[198,80,0],[209,91,2],[217,101,4],[225,111,6],[233,121,7],[241,131,9],[248,141,11],[255,152,14],[255,161,26],[255,170,38],[255,179,49],[255,188,61],[255,197,73],[255,206,84],[255,214,106],[255,220,124],[255,225,142],[255,231,159],[255,237,177],[255,243,195],[255,246,204]];
var STEEL = [[8,10,14],[18,21,26],[27,31,38],[37,42,49],[47,52,61],[58,64,74],[72,79,90],[87,94,106],[102,109,122],[117,124,138],[132,140,153],[147,155,168],[162,170,183],[177,185,198],[192,200,212],[207,213,224],[219,225,235],[232,237,245],[240,243,250],[246,249,255]];
var COPPER = [[22,5,3],[42,11,6],[61,17,9],[81,23,12],[101,30,15],[121,39,19],[140,48,24],[160,57,28],[178,70,36],[195,87,47],[211,103,58],[228,120,69],[237,140,90],[243,161,115],[249,183,139],[255,204,164]];
var ARC = [[4,10,30],[11,26,70],[17,42,110],[25,60,148],[39,84,181],[53,108,214],[76,134,235],[109,163,244],[142,191,253],[173,211,255],[205,229,255],[236,246,255]];

var CW = 640, CH = 368, LOGO_X = 80, LOGO_W = 480, LOGO_H = 160;
var HANDLE_Y = 186, SCROLL_Y = 290, SCROLL_SCALE = 5, SCROLL_SPEED = 130;
var LAND = 0.55;            // seconds until the logo lands
var FRAME_MS = 66;          // a frame is drawn this often: 15 a second
var canvas = null, stars = [], sparks = [], lastTime = 0, nextStrike = 2.5, struck = false;
var big = handle.toUpperCase();
var bigScale = Math.max(2, Math.min(8, Math.floor(620 / (big.length * 6))));
var bigBold = bigScale >> 1;  // each stroke is drawn this much wider
var bigX = Math.floor((CW - (big.length * 6 - 1) * bigScale - bigBold) / 2);
var bigY = HANDLE_Y + Math.floor((8 - bigScale) * 7 / 2);  // smaller lettering sits mid-band
// The greeting fades up once the logo has landed, holds, and fades out;
// the scroller starts when it has gone.
var GREET_AT = LAND + 0.35, GREET_IN = 1.0, GREET_HOLD = 3.0, GREET_OUT = 1.0;
var GREET_END = GREET_AT + GREET_IN + GREET_HOLD + GREET_OUT;
function greetFade(t) {
  var g = t - GREET_AT;
  if (g <= 0 || g >= GREET_END - GREET_AT) return 0;
  if (g < GREET_IN) return g / GREET_IN;
  if (g < GREET_IN + GREET_HOLD) return 1;
  return 1 - (g - GREET_IN - GREET_HOLD) / GREET_OUT;
}
var caption = previous ? 'WELCOME BACK' : 'WELCOME ABOARD';

function step(ramp, i) { return ramp[i < 0 ? 0 : i >= ramp.length ? ramp.length - 1 : i]; }
function rect(x, y, rw, rh, c) { canvas.fillRect(x, y, rw, rh, c[0], c[1], c[2]); }

function drawStars(t) {
  var speed = [14, 34, 78], shade = [7, 12, 18];
  for (var i = 0; i < stars.length; i++) {
    var s = stars[i], layer = i % 3;
    var x = Math.floor(((s[0] - t * speed[layer]) % CW + CW) % CW);
    var twinkle = Math.sin(t * s[2] + i) > 0.86 ? 2 : 0;
    rect(x, s[1], layer === 2 ? 2 : 1, 1, step(STEEL, shade[layer] + twinkle));
  }
}

// Four copper bars swing through the height of the canvas a little apart,
// the nearer ones drawn over the farther. They stay dimmer than the
// lettering in front of them.
function drawBars(t) {
  var bars = [];
  for (var i = 0; i < 4; i++) {
    var phase = t * 1.15 + i * 0.52;
    bars.push({y: Math.round(184 + 162 * Math.sin(phase)), depth: Math.cos(phase), ramp: [AMBER, COPPER, ARC, COPPER][i], top: [25, 11, 5, 9][i]});
  }
  bars.sort(function (a, b) { return a.depth - b.depth; });
  for (var b = 0; b < bars.length; b++) {
    for (var k = 0; k < 20; k++) {
      var y = bars[b].y - 10 + k;
      if (y < 0 || y >= CH) continue;
      rect(0, y, CW, 1, step(bars[b].ramp, Math.round(bars[b].top * Math.sin(Math.PI * (k + 0.5) / 20))));
    }
  }
}

// The logo falls from above the canvas, lands at LAND and rings once.
function logoY(t) {
  if (t < LAND) { var f = t / LAND; return Math.round(-LOGO_H * (1 - f * f)); }
  var after = t - LAND;
  return after < 0.5 ? -Math.round(10 * Math.exp(-after * 9) * Math.abs(Math.sin(after * 19))) : 0;
}

// A band of light crosses the logo's metal every six seconds.
function drawShine(t, top) {
  var phase = (t - LAND - 1.2) % 6;
  if (t < LAND + 1.2 || phase > 1) return;
  var lead = LOGO_X - 30 + (LOGO_W + 110) * phase;
  for (var y = 0; y < LOGO_H; y++) {
    for (var d = -9; d <= 9; d++) {
      var x = Math.round(lead + d - y * 0.5), p = canvas.getPixel(x, top + y);
      if (!p || x < LOGO_X || x >= LOGO_X + LOGO_W || Math.max(p[0], p[1], p[2]) < 60) continue;
      var light = 150 * (1 - Math.abs(d) / 10) * (1 - Math.abs(d) / 10);
      canvas.setPixel(x, top + y, p[0] + light, p[1] + light, p[2] + light);
    }
  }
}

// Text in one flat colour.
function drawFlat(text, x, y, scale, c) {
  for (var i = 0; i < text.length; i++) {
    var rows = glyph(text.charAt(i));
    for (var r = 0; r < 7; r++) for (var b = 0; b < 5; b++) {
      if (rows[r] >> (4 - b) & 1) rect(x + (i * 6 + b) * scale, y + r * scale, scale, scale, c);
    }
  }
}

// One letter of the handle as forged chrome: a shadow, then each pixel row
// shaded dark amber down to a white horizon and ember up to amber below it.
// heat lifts the shading toward white; bold widens strokes; fade dims the
// letter toward black, 1 being full strength.
function drawChrome(ch, x, y, scale, bold, heat, horizon, fade) {
  var rows = glyph(ch), tall = 7 * scale, drop = Math.max(2, scale >> 1), r, b, run;
  for (var pass = 0; pass < 2; pass++) {
    for (r = 0; r < 7; r++) {
      for (b = 0; b < 5; b = run) {
        run = b + 1;
        if (!(rows[r] >> (4 - b) & 1)) continue;
        while (run < 5 && rows[r] >> (4 - run) & 1) run++;
        if (pass === 0) { if (fade > 0.5) rect(x + b * scale + drop, y + r * scale + drop, (run - b) * scale + bold, scale, AMBER[2]); continue; }
        for (var s = 0; s < scale; s++) {
          var f = (r * scale + s) / tall;
          var shade = f < horizon ? 20 + 19 * f / horizon : 9 + 20 * (f - horizon) / (1 - horizon);
          rect(x + b * scale, y + r * scale + s, (run - b) * scale + bold, 1, step(AMBER, Math.round((shade + heat * (39 - shade)) * fade)));
        }
      }
    }
  }
}

function drawHandle(t) {
  var fade = greetFade(t);
  if (fade <= 0) return;
  for (var i = 0; i < big.length; i++) {
    var bob = Math.round(3 * Math.sin(t * 2.4 - i * 0.55));
    var horizon = 0.5 + 0.1 * Math.sin(t * 1.3 + i * 0.4);
    drawChrome(big.charAt(i), bigX + i * 6 * bigScale, bigY + bob, bigScale, bigBold, 0, horizon, fade);
  }
}

// The scroller: each pixel column rides its own point of a sine wave, and
// a band of light runs down the letters.
function drawScroller(t) {
  var pitch = 6 * SCROLL_SCALE;
  var offset = Math.floor(t * SCROLL_SPEED) - CW;   // starts off the right edge
  // Nothing comes before the text on its first pass, so n starts at 0.
  for (var n = Math.max(0, Math.floor(offset / pitch)); n * pitch - offset < CW; n++) {
    var at = n % message.length;
    var rows = glyph(message.charAt(at)), ramp = marks.charAt(at) === '1' ? STEEL : AMBER;
    var low = ramp === STEEL ? 9 : 17, span = ramp === STEEL ? 10 : 21;
    for (var b = 0; b < 5; b++) {
      var x = n * pitch - offset + b * SCROLL_SCALE;
      if (x <= -SCROLL_SCALE || x >= CW) continue;
      var y = SCROLL_Y + Math.round(21 * Math.sin(x * 0.017 + t * 2.9));
      for (var r = 0; r < 7; r++) {
        if (!(rows[r] >> (4 - b) & 1)) continue;
        var light = Math.round(5 * (0.5 + 0.5 * Math.sin(r * 0.8 - t * 4.5))) / 5;
        rect(x, y + r * SCROLL_SCALE, SCROLL_SCALE, SCROLL_SCALE, step(ramp, low + Math.round(span * light)));
      }
    }
  }
}

// Sparks: thrown up and outward from a strike, pulled down, cooling from
// white through amber to red as they go.
function burst(x, y, count, force) {
  for (var i = 0; i < count; i++) {
    var angle = -Math.PI * (0.12 + 0.76 * Math.random()), v = force * (0.35 + 0.65 * Math.random());
    sparks.push({x: x, y: y, vx: Math.cos(angle) * v, vy: Math.sin(angle) * v, age: 0, life: 0.5 + 0.9 * Math.random()});
  }
}
function drawSparks(dt) {
  var alive = [];
  for (var i = 0; i < sparks.length; i++) {
    var s = sparks[i], px = s.x, py = s.y;
    s.age += dt;
    if (s.age >= s.life || s.y > CH) continue;
    s.vy += 330 * dt; s.x += s.vx * dt; s.y += s.vy * dt;
    var cool = s.age / s.life, c = step(AMBER, Math.round(39 - 25 * cool));
    var size = cool < 0.25 ? 3 : cool < 0.65 ? 2 : 1;
    rect(Math.round(px), Math.round(py), size > 1 ? 2 : 1, size > 1 ? 2 : 1, step(AMBER, Math.round(28 - 14 * cool)));
    rect(Math.round(s.x), Math.round(s.y), size, size, c);
    alive.push(s);
  }
  sparks = alive;
}
// A hammer blow somewhere along the foot of the handle.
function strike(t) {
  if (t < nextStrike) return;
  nextStrike = t + 2.2 + 2.6 * Math.random();
  var x = bigX + Math.random() * (big.length * 6 - 1) * bigScale, y = bigY + 7 * bigScale;
  burst(x, y, 26, 210);
  rect(Math.round(x) - 7, y - 1, 15, 2, ARC[11]);
  rect(Math.round(x) - 1, y - 6, 2, 11, ARC[11]);
}

function drawFrame(t) {
  var dt = Math.min(0.12, t - lastTime), top = logoY(t);
  lastTime = t;
  canvas.clear(0, 0, 0);
  drawStars(t);
  drawBars(t);
  canvas.drawImage('hello-vision3-logo.png', LOGO_X, top);
  drawShine(t, top);
  if (!struck && t >= LAND) {
    struck = true;
    for (var i = 0; i < 44; i++) burst(LOGO_X + 20 + Math.random() * (LOGO_W - 40), LOGO_H - 12, 1, 240);
  }
  var fade = greetFade(t);
  if (fade > 0) drawFlat(caption, Math.floor((CW - (caption.length * 6 - 1) * 2) / 2), 166, 2, step(STEEL, Math.round(11 * fade)));
  drawHandle(t);
  if (t >= GREET_END) drawScroller(t - GREET_END);
  if (fade >= 1) strike(t);
  drawSparks(dt);
}

// The tune is scripts/data/hello-vision3-tune.b64: a WAV file (8-bit unsigned
// PCM, 22050 Hz, mono) as base64 text. It is sent after the first frame, not
// before it, so the caller's link carries the picture first; it loops until
// the door ends. Sound plays only where v3.gfx.audio is true, the terminal's
// answer to the audio query at connect; with it false, or the file missing or
// not a WAV, the door writes no audio bytes and carries on silently. The
// sequences are the login matrix's: LoadBlob into a slot, Queue to play (L
// loops), Flush to stop. Slot 253 and channel 3 are not the matrix's.
var tune = false, tuneTried = false;
function startTune() {
  if (tuneTried) return;
  tuneTried = true;
  if (!(v3.gfx && v3.gfx.audio)) return;
  var b64;
  try { b64 = v3.fs.read('hello-vision3-tune.b64').trim(); } catch (e) { return; }
  // Base64 text of a WAV: "RIFF", a size, "WAVE" encode to UklGR..., then
  // QVZF at 12; the length is a whole number of 4-character groups.
  if (b64.length < 64 || b64.length % 4 !== 0 || b64.indexOf('UklGR') !== 0 || b64.substr(12, 4) !== 'QVZF' ||
      !/^[A-Za-z0-9+\/]+={0,2}$/.test(b64)) return;
  v3.console.write('\x1b_SyncTERM:A;LoadBlob;S=253;' + b64 + '\x1b\\' + '\x1b_SyncTERM:A;Queue;C=3;S=253;L\x1b\\');
  tune = true;
}
function stopTune() {
  // O= is a fade-out in milliseconds, so the tune dies away with the door.
  if (tune) v3.console.write('\x1b_SyncTERM:A;Flush;C=3;O=900\x1b\\');
  tune = false;
}

// Runs the picture until a key. Returns false, having drawn nothing, when
// the canvas, its palette or the logo cannot be had.
function picture() {
  var left = Math.floor((w - 80) / 2) + 1;
  try {
    canvas = v3.gfx.canvas(CW, CH, 'hello-vision3-palette.png');
    canvas.clear(0, 0, 0);
    canvas.drawImage('hello-vision3-logo.png', LOGO_X, 0);
  } catch (err) {
    return false;
  }
  for (var i = 0; i < 96; i++) stars.push([Math.random() * CW, Math.floor(Math.random() * CH), 1.5 + 3 * Math.random()]);
  v3.console.write('\x1b[0m\x1b[?25l');
  v3.console.clear();
  var hint = 'any key returns to the menu';
  v3.console.gotoxy(Math.floor((w - hint.length) / 2) + 1, 24);
  v3.console.write('\x1b[1;30m' + hint + '\x1b[0m');
  var began = Date.now();
  for (;;) {
    var frameStart = Date.now();
    drawFrame((frameStart - began) / 1000);
    v3.console.gotoxy(left, 1);
    // presentDelta sends the whole picture first and changed pixels after;
    // the board holds a frame back until the terminal has taken the last.
    canvas.presentDelta();
    startTune();
    var key = v3.console.readkey(Math.max(1, FRAME_MS - (Date.now() - frameStart)));
    if (key || !v3.session.online) return true;
  }
}

// ---------------------------------------------------------------------------
// The ANSI version: 16 colours, CP437's blocks.

var RED = '\x1b[0;31m', BROWN = '\x1b[0;33m', YELLOW = '\x1b[1;33m', WHITE = '\x1b[1;37m', GREY = '\x1b[0;37m', DARK = '\x1b[1;30m';
var FIRE = [WHITE, YELLOW, YELLOW, BROWN, RED, BROWN, YELLOW];
// FIGlet Graffiti, as the main menu's text logo.
var TEXT_LOGO = ["____   ____.__  _________.__________    _______        /\\ ________", "\\   \\ /   /|__|/   _____/|__\\_____  \\   \\      \\      / / \\_____  \\", " \\   Y   / |  |\\_____  \\ |  |/   |   \\  /   |   \\    / /    _(__  <", "  \\     /  |  |/        \\|  /    |    \\/    |    \\  / /    /       \\", "   \\___/   |__/_______  /|__\\_______  /\\____|__  / / /    /______  /", "                      \\/            \\/         \\/  \\/            \\/"];

function put(x, y, text) { v3.console.gotoxy(x, y); v3.console.write(text); }

// The logo: hot on the left, steel on the right, as the picture is. glint
// is the column a light is passing, or -9.
function textLogo(glint) {
  for (var y = 0; y < TEXT_LOGO.length; y++) {
    var line = TEXT_LOGO[y], out = '', last = '';
    for (var x = 0; x < line.length; x++) {
      var c = Math.abs(x - y - glint) < 2 ? WHITE : x < 18 ? YELLOW : x < 36 ? BROWN : x < 44 ? RED : x < 56 ? DARK : GREY;
      if (c !== last && line.charAt(x) !== ' ') { out += c; last = c; }
      out += line.charAt(x);
    }
    put(7, 2 + y, out);
  }
}

// Rows of half blocks for text in the 5x7 font: four rows of characters,
// six columns a letter. Each entry is a row of cells: ' ', or a block.
function blockRows(text) {
  var out = ['', '', '', ''], cells = [' ', '\u2580', '\u2584', '\u2588'];
  for (var i = 0; i < text.length; i++) {
    var rows = glyph(text.charAt(i));
    for (var r = 0; r < 4; r++) {
      for (var b = 0; b < 5; b++) {
        var upper = rows[r * 2] >> (4 - b) & 1, lower = r < 3 ? rows[r * 2 + 1] >> (4 - b) & 1 : 0;
        out[r] += cells[upper + lower * 2];
      }
      if (i < text.length - 1) out[r] += ' ';
    }
  }
  return out;
}

// The handle as lines of block lettering: one line when it fits in 13
// letters, two when it splits at a space into two that do, else none and
// the handle is written as spaced capitals.
function blockLines(name) {
  if (name.length <= 13) return [name];
  for (var gap = name.indexOf(' '); gap > 0; gap = name.indexOf(' ', gap + 1)) {
    if (gap <= 13 && name.length - gap - 1 <= 13) return [name.slice(0, gap), name.slice(gap + 1)];
  }
  return [];
}

// One row of the handle. The fire colours run down the rows of the
// lettering, band being the row's place in them, and a white light crosses
// at column glint, leaning by slant. With spread set the colours run along
// the row instead, for a handle written on one line.
function cycled(row, x0, slant, band, glint, spread) {
  var out = '', last = '';
  for (var x = 0; x < row.length; x++) {
    var c = Math.abs(x0 + x + slant - glint) < 2 ? WHITE : FIRE[((band + (spread ? x >> 1 : 0)) % FIRE.length + FIRE.length) % FIRE.length];
    if (c !== last && row.charAt(x) !== ' ') { out += c; last = c; }
    out += row.charAt(x);
  }
  return out;
}

// The wave scroller: three rows; column x shows one character of the
// message on the row its point of the wave is at. Each row is written from
// its first character to its last, and the rest erased.
function textScroller(t, top) {
  var shift = Math.floor(t * 11) - 78, rows = [[], [], []];
  for (var x = 0; x < 78; x++) {
    if (shift + x < 0) continue;
    var at = (shift + x) % message.length, ch = message.charAt(at);
    if (ch === ' ') continue;
    var r = 1 + Math.round(1.2 * Math.sin(x * 0.08 + t * 1.7));
    rows[r].push([x, ch, marks.charAt(at) === '1' ? [WHITE, GREY, DARK][r] : [YELLOW, BROWN, RED][r]]);
  }
  for (var y = 0; y < 3; y++) {
    var out = '', col = 0, last = '';
    for (var i = 0; i < rows[y].length; i++) {
      var cell = rows[y][i];
      while (col < cell[0]) { out += ' '; col++; }
      if (cell[2] !== last) { out += cell[2]; last = cell[2]; }
      out += cell[1]; col++;
    }
    put(2, top + y, out + '\x1b[K');
  }
}

// A rule with a light running along it. Only the cells the light is on,
// or was on when the rule was last drawn, are written; lit holds each
// rule's last span. The light is off the rule for part of each lap.
var lit = {};
function rule(y, t) {
  var spot = Math.floor(t * 30) % 140, was = lit[y] || [78, -1];
  var now = [Math.max(0, spot - 7), Math.min(77, spot)];
  if (now[0] > now[1]) now = [78, -1];
  var from = Math.min(was[0], now[0]), to = Math.max(was[1], now[1]), out = '';
  lit[y] = now;
  for (var x = from; x <= to; x++) {
    var near = spot - x;
    out += (near < 0 || near > 7 ? DARK : near < 2 ? WHITE : near < 4 ? YELLOW : BROWN) + '\u2500';
  }
  if (out) put(2 + from, y, out);
}
function line78() { var out = ''; for (var x = 0; x < 78; x++) out += '\u2500'; return out; }

function text() {
  v3.console.write('\x1b[0m');
  v3.console.clear();
  if (w < 80 || h < 24) {
    // Too small for the layout: the greeting and the details as lines.
    // The handle and the board's name are written as they are, so a '|'
    // in one is not read as a colour code.
    v3.console.print('|14Hello, |15'); v3.console.write(handle); v3.console.println('|14!');
    v3.console.print('|07Call |15' + calls + '|07 on node |15' + node + '|07 of |15'); v3.console.write(board); v3.console.println('|07.');
    v3.console.println(previous ? '|07Your last call was |15' + previous + '|07.' : '|07This is your first call.');
    v3.console.println('|08any key returns to the menu');
    startTune();
    while (!v3.console.readkey(1000) && v3.session.online) { /* wait for a key */ }
    return;
  }
  v3.console.write('\x1b[?25l');
  var lines = blockLines(big), blocks = [], top = lines.length === 2 ? 9 : 11;
  for (var i = 0; i < lines.length; i++) blocks.push(blockRows(lines[i]));
  var spaced = big.split('').join(' ');
  if (lines.length < 2) put(Math.floor((80 - caption.length * 2 + 1) / 2) + 1, 9, DARK + caption.split('').join(' '));
  put(Math.floor((80 - 27) / 2) + 1, 24, DARK + 'any key returns to the menu');
  textLogo(-9);
  put(2, 8, DARK + line78()); put(2, 17, DARK + line78());
  var began = Date.now(), frame = 0, logoLit = false;
  for (;;) {
    var t = (Date.now() - began) / 1000;
    // The scroller moves every frame; the colours on the handle and the
    // rules, and the light on the logo, every other.
    textScroller(t, 20);
    if (frame % 2 === 0) {
      var phase = Math.floor(t * 5), glint = (t % 4) * 60 - 12;
      for (var b = 0; b < blocks.length; b++) for (var r = 0; r < 4; r++) {
        var x0 = Math.floor((80 - blocks[b][r].length) / 2) + 1;
        put(x0, top + b * 4 + r, cycled(blocks[b][r], x0, b * 4 + r, b * 4 + r - phase, glint, false));
      }
      if (!lines.length) put(Math.max(1, Math.floor((80 - spaced.length) / 2) + 1), 12, cycled(spaced.slice(0, 79), 1, 0, -phase, glint, true));
      rule(8, t); rule(17, t + 2.3);
      var sweep = (t % 7) * 60;
      if (sweep < 90) { textLogo(sweep - 6); logoLit = true; }
      else if (logoLit) { textLogo(-9); logoLit = false; }
    }
    frame++;
    startTune();
    var key = v3.console.readkey(90);
    if (key || !v3.session.online) return;
  }
}

// The tune stops however the door ends, an error included.
try {
  if (!(v3.gfx && v3.gfx.sixel && w >= 80 && h >= 24 && picture())) text();
} finally {
  stopTune();
}
v3.console.write('\x1b[0m\x1b[?25h');
v3.console.clear();
