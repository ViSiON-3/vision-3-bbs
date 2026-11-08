// Text viewer: what V opens on a text file in the file lists, VIEW_FILE and
// TYPE_TEXT_FILE, in place of the built-in pager, when config.json's
// textViewerDoor names the TEXTVIEW door (doors.json, hidden).
//
// The BBS passes the file's area tag and filename as the last two
// arguments. The file is read with v3.file.read, which needs the area's
// download access. ANSI files are drawn with v3.ansi.render; other text is
// wrapped to the screen. What counts as text is kindOf's call, below: a
// file it can't place is shown if the caller says so, and binary files are
// refused.
//
// Keys: Up/Down a line, PgUp/PgDn (or Space) a screen, Home/End, Left/Right
// and Enter on the command bar, or the command's hotkey. Find asks for text;
// Enter on Find looks for the same text again. Tag adds the file to the
// download batch (or takes it out). Prev/Next open the previous or next text
// file in the area, passing over any kindOf can't place. Esc or Q leaves.
//
// To change the look, edit COMMANDS, COLORS and BOTTOM_RULE below.

// BOTTOM_RULE draws a rule above the command bar, matching the one under
// the header. Set it to false for one more row of text on short screens.
var BOTTOM_RULE = true;

var COMMANDS = [
    { id: "more", label: "More", key: "m" },
    { id: "back", label: "Back", key: "b" },
    { id: "home", label: "Home", key: "h" },
    { id: "end", label: "End", key: "e" },
    { id: "find", label: "Find", key: "f" },
    { id: "tag", label: "Tag", key: "t" },
    { id: "prev", label: "Prev", key: "p" },
    { id: "next", label: "Next", key: "n" },
    { id: "quit", label: "Quit", key: "q" }
];

// SGR sequences. barHi and bar match the stock file list command bar
// (FILELISTCMD.BAR: 31 and 7).
var COLORS = {
    title: "\x1b[0;1;37m",
    info: "\x1b[0;36m",
    desc: "\x1b[0;37m",
    rule: "\x1b[0;34m",
    text: "\x1b[0;37m",
    barHi: "\x1b[0;1;37;44m",
    bar: "\x1b[0;37m",
    status: "\x1b[0;1;33m"
};

// --- What the viewers can show. This block is the same in textview.js and
// artview.js; keep the two in step (a test checks they match).

// Files that are text by name: these extensions, and README or FILE_ID
// without one. Others are judged by their contents alone.
var TEXT_EXT = /\.(ans|asc|ice|diz|nfo|txt|doc|lit|1st|me|now|ad|log|mem|lst|faq|msg|inf|bbs|pcb|cfg|ini|bat|md)$/i;
var TEXT_NAME = /(^|[\/\\])(readme|file_id)$/i;

function textByName(name) {
    return TEXT_EXT.test(name) || TEXT_NAME.test(name);
}

// kindOf says how data, from the file called name, can be shown, judging
// the text before its Ctrl-Z (a SAUCE record and NUL padding follow it):
//   "binary": it has NUL bytes, or control bytes make up a quarter of it;
//   "text":   it has no control bytes, or its name says it's text (CP437 art
//             draws bytes 1-31 as glyphs: ☺ ♥ ► ▲ ▼ and the rest);
//   "ask":    a few control bytes and a name that says nothing, so the
//             caller is asked whether to view it anyway.
// Tab, CR, LF and ESC don't count as control bytes.
function kindOf(name, data) {
    var end = data.indexOf("\x1a");
    var sample = data.slice(0, end >= 0 ? Math.min(end, 4096) : 4096);
    var ctl = 0;
    for (var i = 0; i < sample.length; i++) {
        var c = sample.charCodeAt(i);
        if (c === 0) return "binary";
        if (c < 32 && c !== 9 && c !== 10 && c !== 13 && c !== 27) ctl++;
    }
    if (ctl * 4 > sample.length) return "binary";
    return ctl === 0 || textByName(name) ? "text" : "ask";
}

// --- End of the shared block.

var ARCHIVE = /\.zip$/i;
var MAX_LINES = 100000;

function sizeText(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + "M";
    if (n >= 1024) return Math.round(n / 1024) + "K";
    return n + "b";
}

// stripSGR removes escape sequences, for searching what a line shows.
function stripSGR(s) {
    return s.replace(/\x1b\[[0-9;?=]*[@-~]/g, "");
}

// wrapText splits plain text into lines no wider than width.
function wrapText(text, width) {
    var out = [];
    var src = text.replace(/\r\n?/g, "\n").replace(/\t/g, "    ").split("\n");
    for (var i = 0; i < src.length && out.length < MAX_LINES; i++) {
        var line = src[i];
        if (line === "") {
            out.push("");
            continue;
        }
        while (line.length > width) {
            var cut = line.lastIndexOf(" ", width);
            if (cut <= 0) cut = width;
            out.push(line.slice(0, cut));
            line = line.slice(cut).replace(/^ /, "");
        }
        out.push(line);
    }
    while (out.length && out[out.length - 1] === "") out.pop();
    return out;
}

function TextView(tag, name) {
    this.area = v3.file.area(tag);
    this.files = this.area ? v3.file.list(this.area.id) : [];
    this.at = -1;
    for (var i = 0; i < this.files.length; i++) {
        if (this.files[i].filename.toLowerCase() === String(name).toLowerCase()) this.at = i;
    }
    this.cmd = 0;
    this.top = 0;
    this.lines = [];
    this.search = "";
    this.hit = -1; // line of the last Find match
    this.status = "";
}

TextView.prototype.width = function () {
    return Math.max(40, Math.min(v3.console.width || 80, 132));
};

TextView.prototype.height = function () {
    return Math.max(10, v3.console.height || 24);
};

// page is how many text rows fit between the header and the command bar
// (and the rule above it, with BOTTOM_RULE).
TextView.prototype.page = function () {
    return this.height() - (BOTTOM_RULE ? 5 : 4);
};

// load reads file index into lines, or returns why it can't be shown. A
// file kindOf can't place is shown only when ask is set and the caller says
// so; "" from the question, as from success, means nothing more to say.
TextView.prototype.load = function (index, ask) {
    var f = this.files[index];
    if (ARCHIVE.test(f.filename)) return "an archive";
    var data = v3.file.read(this.area.id, f.filename);
    if (data === null) return f.size > 4194304 ? "too big to view" : "no access to view it";
    var kind = kindOf(f.filename, data);
    if (kind === "binary") return "not a text file";
    if (kind === "ask") {
        if (!ask) return "format unknown";
        v3.console.write(COLORS.status + f.filename + ": format unknown. View anyway? (Y/N) \x1b[0m");
        if (v3.console.readkey().toLowerCase() !== "y") return "no";
        v3.console.clear();
    }
    // Ctrl-Z ends the text; a SAUCE record and its NUL padding follow it.
    var end = data.indexOf("\x1a");
    if (end >= 0) data = data.slice(0, end);
    if (data.indexOf("\x1b[") >= 0) {
        this.lines = v3.ansi.render(data, { width: Math.min(this.width(), 80), rows: 4000 });
    } else {
        this.lines = wrapText(v3.ansi.glyphs(data), this.width() - 1);
    }
    this.at = index;
    this.top = 0;
    this.hit = -1;
    return "";
};

TextView.prototype.drawHeader = function () {
    var f = this.files[this.at];
    var w = this.width();
    var right = sizeText(f.size) + "  " + (this.at + 1) + "/" + this.files.length;
    if (v3.file.isMarked(this.area.id, f.filename)) right = "Tagged  " + right;
    var name = f.filename.slice(0, w - right.length - 3);
    v3.console.gotoxy(1, 1);
    v3.console.write(COLORS.title + " " + name + COLORS.info +
        new Array(Math.max(1, w - name.length - right.length - 1)).join(" ") + right + "\x1b[K");
    var desc = String(f.description || "").split("\n")[0].slice(0, w - 2);
    v3.console.gotoxy(1, 2);
    v3.console.write(COLORS.desc + " " + desc + "\x1b[K");
    v3.console.gotoxy(1, 3);
    v3.console.write(COLORS.rule + new Array(w).join("─") + "\x1b[K");
};

TextView.prototype.drawBody = function () {
    var rows = this.page();
    for (var i = 0; i < rows; i++) {
        var line = this.lines[this.top + i];
        v3.console.gotoxy(1, 4 + i);
        v3.console.write(COLORS.text + (line === undefined ? "" : line) + "\x1b[0m\x1b[K");
    }
};

// drawBar draws the command bar and status on the bottom row, kept short of
// the last column so the row never wraps and scrolls the screen. On a narrow
// screen the commands scroll to keep the selected one in view, and the
// status is cut to the room that's left.
TextView.prototype.drawBar = function () {
    var room = Math.min(this.width(), v3.console.width || 80) - 1;
    var cmdWidth = function (i) { return COMMANDS[i].label.length + 2; };
    var first = 0, used = 0, i;
    for (i = this.cmd; i >= 0 && used + cmdWidth(i) <= room; i--) {
        used += cmdWidth(i);
        first = i;
    }
    var out = "";
    used = 0;
    for (i = first; i < COMMANDS.length && used + cmdWidth(i) <= room; i++) {
        out += (i === this.cmd ? COLORS.barHi : COLORS.bar) + " " + COMMANDS[i].label + " ";
        used += cmdWidth(i);
    }
    var shown = this.lines.length ? Math.min(100, Math.round((this.top + this.page()) * 100 / this.lines.length)) : 100;
    var status = (this.status || shown + "%").slice(0, Math.max(0, room - used - 2));
    if (BOTTOM_RULE) {
        v3.console.gotoxy(1, this.height() - 1);
        v3.console.write(COLORS.rule + new Array(this.width()).join("─") + "\x1b[0m\x1b[K");
    }
    if (status !== "") out += COLORS.bar + "  " + (this.status ? COLORS.status : "") + status;
    v3.console.gotoxy(1, this.height());
    v3.console.write(out + "\x1b[0m\x1b[K");
    this.status = "";
};

TextView.prototype.draw = function () {
    this.drawHeader();
    this.drawBody();
    this.drawBar();
};

TextView.prototype.scrollTo = function (top) {
    var last = Math.max(0, this.lines.length - this.page());
    this.top = Math.max(0, Math.min(top, last));
};

// step opens the next (dir 1) or previous (dir -1) text file in the area.
TextView.prototype.step = function (dir) {
    for (var i = this.at + dir; i >= 0 && i < this.files.length; i += dir) {
        if (this.load(i) === "") return;
    }
    this.status = dir > 0 ? "No more text files" : "No earlier text files";
};

// find looks for the search text, asking for it first when ask is set. A
// repeated Find carries on after the last match while that match is still on
// screen (near the end of a file the screen can't scroll to put it on top);
// otherwise it starts at the top of the screen.
TextView.prototype.find = function (ask) {
    if (ask || this.search === "") {
        v3.console.gotoxy(1, this.height());
        v3.console.write(COLORS.status + " Find: " + COLORS.text + "\x1b[K");
        var text = v3.console.getstr(Math.max(1, Math.min(40, this.width() - 8)));
        if (text === "") return;
        this.search = text;
        this.hit = -1;
    }
    var onScreen = this.hit >= this.top && this.hit < this.top + this.page();
    var from = onScreen ? this.hit + 1 : this.top;
    var want = this.search.toLowerCase();
    for (var i = from; i < this.lines.length; i++) {
        if (stripSGR(this.lines[i]).toLowerCase().indexOf(want) >= 0) {
            this.hit = i;
            this.scrollTo(i);
            this.status = "Found on line " + (i + 1);
            return;
        }
    }
    this.status = "Not found: " + this.search;
};

TextView.prototype.toggleTag = function () {
    var f = this.files[this.at];
    if (v3.file.isMarked(this.area.id, f.filename)) {
        v3.file.unmark(this.area.id, f.filename);
        this.status = "Untagged";
    } else if (v3.file.mark(this.area.id, f.filename)) {
        this.status = "Tagged for download";
    } else {
        this.status = "Can't tag this file";
    }
};

// run carries out command id; false means leave.
TextView.prototype.run = function (id, fromEnter) {
    var page = this.page();
    switch (id) {
        case "more": this.scrollTo(this.top + page); break;
        case "back": this.scrollTo(this.top - page); break;
        case "home": this.scrollTo(0); break;
        case "end": this.scrollTo(this.lines.length); break;
        case "find": this.find(!fromEnter); break;
        case "tag": this.toggleTag(); break;
        case "prev": this.step(-1); break;
        case "next": this.step(1); break;
        case "quit": return false;
    }
    return true;
};

TextView.prototype.loop = function () {
    this.draw();
    for (;;) {
        var k = v3.console.readkey();
        if (!v3.session.online || k === "") return;
        var go = true;
        switch (k) {
            case "UP": this.scrollTo(this.top - 1); break;
            case "DOWN": this.scrollTo(this.top + 1); break;
            case "PGDN": case " ": go = this.run("more"); break;
            case "PGUP": go = this.run("back"); break;
            case "HOME": go = this.run("home"); break;
            case "END": go = this.run("end"); break;
            case "LEFT": this.cmd = (this.cmd + COMMANDS.length - 1) % COMMANDS.length; break;
            case "RIGHT": this.cmd = (this.cmd + 1) % COMMANDS.length; break;
            case "\r": go = this.run(COMMANDS[this.cmd].id, true); break;
            case "ESC": go = false; break;
            default:
                for (var i = 0; i < COMMANDS.length; i++) {
                    if (k.toLowerCase() === COMMANDS[i].key) {
                        this.cmd = i;
                        go = this.run(COMMANDS[i].id);
                    }
                }
        }
        if (!go) return;
        this.draw();
    }
};

TextView.prototype.main = function () {
    v3.console.clear();
    var why = this.at < 0 ? "That file isn't in the file base." : this.load(this.at, true);
    if (why === "no") {
        v3.console.clear();
        return;
    }
    if (why !== "") {
        if (this.at >= 0) why = this.files[this.at].filename + ": " + why + ".";
        v3.console.writeln(COLORS.status + why + "\x1b[0m");
        v3.console.pause();
        return;
    }
    this.loop();
    v3.console.write("\x1b[0m");
    v3.console.clear();
};

new TextView(v3.args[v3.args.length - 2], v3.args[v3.args.length - 1]).main();
