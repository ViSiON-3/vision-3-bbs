// Archive viewer: what V opens on a ZIP in the file lists and VIEW_FILE, in
// place of ZipLab's contents list, when config.json's archiveViewerDoor
// names the ARTVIEW door (doors.json, hidden).
//
// The BBS passes the archive's area tag and filename as the last two
// arguments. A lightbar lists the archive's entries with each piece's SAUCE
// title and artist. Enter shows a piece: ANSI art is drawn as ANSI.SYS
// would (v3.ansi.render) and shown a screen at a time, text likewise. What
// counts as art or text is kindOf's call, below, made for every entry when
// the archive is opened from the sample v3.file.members gives each (entries
// without one are greyed by name until read). An entry it can't place is
// shown if the caller says so, and binary entries are refused.
// Entries are read in memory with v3.file.readMember, which needs the
// area's download access; nothing is extracted to disk.
//
// In the list: Up/Down, PgUp/PgDn, Home/End, Enter to view, T to tag the
// archive for download, Esc or Q to leave. Viewing a piece: Up/Down a line,
// PgUp/PgDn (or Space) a screen, Home/End, N/P the next or previous piece,
// T to tag, Esc or Q back to the list.
//
// To change the look, edit COLORS and BOTTOM_RULE below.

// SAMPLE_BYTES is how much of each entry v3.file.members hands over for
// kindOf, which looks at no more than the first 4096 bytes.
var SAMPLE_BYTES = 4096;

// BOTTOM_RULE draws a rule above the footer, matching the one under the
// header. Set it to false for one more row of art on short screens.
var BOTTOM_RULE = true;

var COLORS = {
    title: "\x1b[0;1;37m",
    info: "\x1b[0;36m",
    rule: "\x1b[0;34m",
    item: "\x1b[0;37m",
    itemHi: "\x1b[0;1;37;44m",
    sauce: "\x1b[0;36m",
    other: "\x1b[0;1;30m",
    help: "\x1b[0;37m",
    key: "\x1b[0;1;37m",
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
//   "binary": it has NUL bytes, or control bytes make up a quarter or more;
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
    if (ctl > 0 && ctl * 4 >= sample.length) return "binary";
    return ctl === 0 || textByName(name) ? "text" : "ask";
}

// --- End of the shared block.

function sizeText(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + "M";
    if (n >= 1024) return Math.round(n / 1024) + "K";
    return n + "b";
}

function pad(s, n) {
    s = String(s);
    return s.length >= n ? s.slice(0, n) : s + new Array(n - s.length + 1).join(" ");
}

function ArtView(tag, name) {
    this.area = v3.file.area(tag);
    this.name = name;
    this.entries = [];
    this.sauce = {};
    this.kinds = {}; // kindOf for each entry, from members' sample or once read
    this.sel = 0;
    this.top = 0;
    this.status = "";
}

ArtView.prototype.width = function () {
    return Math.max(40, Math.min(v3.console.width || 80, 132));
};

ArtView.prototype.height = function () {
    return Math.max(10, v3.console.height || 24);
};

// rows is how many rows fit between the two-row header and the footer (and
// the rule above it, with BOTTOM_RULE).
ArtView.prototype.rows = function () {
    return this.height() - (BOTTOM_RULE ? 4 : 3);
};

// viewable reports whether an entry is art or text: by its contents once
// kindOf has seen them (from members' sample, or once read), by its name
// before.
ArtView.prototype.viewable = function (e) {
    if (e.readable === false) return false;
    var kind = this.kinds[e.name];
    return kind ? kind === "text" : textByName(e.name);
};

// kindOfEntry returns kindOf for an entry, or "unreadable" when it can't be
// read. Most entries have their answer from members' sample already; others
// (past members' sample budget) are read here, once.
ArtView.prototype.kindOfEntry = function (e) {
    if (!(e.name in this.kinds)) {
        var data = e.readable === false ? null : v3.file.readMember(this.area.id, this.name, e.name);
        this.kinds[e.name] = data === null ? "unreadable" : kindOf(e.name, data);
        if (data !== null && this.kinds[e.name] === "text") this.sauce[e.name] = v3.ansi.sauce(data);
    }
    return this.kinds[e.name];
};

// sauceOf reads and remembers an entry's SAUCE record (null for none). It
// reads only entries named as text; others get theirs once read to be
// shown (show and kindOfEntry fill it in).
ArtView.prototype.sauceOf = function (e) {
    if (!(e.name in this.sauce)) {
        var data = textByName(e.name) ? v3.file.readMember(this.area.id, this.name, e.name) : null;
        this.sauce[e.name] = data === null ? null : v3.ansi.sauce(data);
    }
    return this.sauce[e.name];
};

ArtView.prototype.header = function (left) {
    var w = this.width();
    var right = this.entries.length + " entries";
    if (v3.file.isMarked(this.area.id, this.name)) right = "Tagged  " + right;
    left = left.slice(0, w - right.length - 3);
    v3.console.gotoxy(1, 1);
    v3.console.write(COLORS.title + " " + left + COLORS.info +
        new Array(Math.max(1, w - left.length - right.length - 1)).join(" ") + right + "\x1b[K");
    v3.console.gotoxy(1, 2);
    v3.console.write(COLORS.rule + new Array(w).join("─") + "\x1b[K");
};

ArtView.prototype.footer = function (help) {
    if (BOTTOM_RULE) {
        v3.console.gotoxy(1, this.height() - 1);
        v3.console.write(COLORS.rule + new Array(this.width()).join("─") + "\x1b[0m\x1b[K");
    }
    v3.console.gotoxy(1, this.height());
    var text = this.status ? COLORS.status + " " + this.status : COLORS.help + " " + help;
    v3.console.write(text.replace(/\[(\w+)\]/g, COLORS.key + "$1" + COLORS.help) + "\x1b[0m\x1b[K");
    this.status = "";
};

ArtView.prototype.drawList = function () {
    this.header(this.name);
    var w = this.width();
    var rows = this.rows();
    if (this.sel < this.top) this.top = this.sel;
    if (this.sel >= this.top + rows) this.top = this.sel - rows + 1;
    for (var i = 0; i < rows; i++) {
        var e = this.entries[this.top + i];
        v3.console.gotoxy(1, 3 + i);
        if (!e) {
            v3.console.write("\x1b[0m\x1b[K");
            continue;
        }
        var s = this.sauceOf(e);
        var about = s ? [s.title, s.author && "by " + s.author, s.group && "/ " + s.group].filter(Boolean).join(" ") : "";
        var line = " " + pad(e.name, 16) + " " + pad(sizeText(e.size), 6) + " ";
        var color = this.viewable(e) ? COLORS.item : COLORS.other;
        if (this.top + i === this.sel) {
            v3.console.write(COLORS.itemHi + pad(line + about, w - 1) + "\x1b[0m\x1b[K");
        } else {
            v3.console.write(color + line + COLORS.sauce + about.slice(0, w - line.length - 1) + "\x1b[0m\x1b[K");
        }
    }
    this.footer("[Enter] view  [T]ag  [Esc] leave");
};

ArtView.prototype.toggleTag = function () {
    if (v3.file.isMarked(this.area.id, this.name)) {
        v3.file.unmark(this.area.id, this.name);
        this.status = "Untagged";
    } else if (v3.file.mark(this.area.id, this.name)) {
        this.status = "Tagged for download";
    } else {
        this.status = "Can't tag this file";
    }
};

// show displays entry index a screen at a time. It returns "next", "prev"
// or "list", or "" when the caller has gone. An entry kindOf can't place is
// shown only when ask is set and the caller says so.
ArtView.prototype.show = function (index, ask) {
    var e = this.entries[index];
    // members says when an entry can't be unpacked at all, and why.
    if (e.readable === false) {
        this.status = e.name + (e.encrypted ? ": encrypted" : ": compression method not supported");
        return "list";
    }
    var data = v3.file.readMember(this.area.id, this.name, e.name);
    var kind = data === null ? "unreadable" : kindOf(e.name, data);
    this.kinds[e.name] = kind;
    if (kind === "unreadable") {
        this.status = e.name + ": can't be read";
        return "list";
    }
    if (kind === "binary") {
        this.status = e.name + ": not art or text";
        return "list";
    }
    if (kind === "ask") {
        if (!ask) return "list";
        v3.console.gotoxy(1, this.height());
        v3.console.write(COLORS.status + " " + e.name + ": format unknown. View anyway? (Y/N) \x1b[0m\x1b[K");
        var yes = v3.console.readkey().toLowerCase() === "y";
        if (!v3.session.online) return "";
        if (!yes) return "list";
    }
    var lines = v3.ansi.render(data, { width: Math.min(this.width(), 80), rows: 4000 });
    // The data is in hand, so its SAUCE is too, whatever the entry's name.
    this.sauce[e.name] = v3.ansi.sauce(data);
    var s = this.sauce[e.name];
    var title = e.name + (s && s.title ? "  " + s.title : "") + (s && s.author ? " by " + s.author : "");
    var page = this.rows();
    var top = 0;
    var last = Math.max(0, lines.length - page);
    v3.console.clear();
    for (;;) {
        this.header(title);
        for (var i = 0; i < page; i++) {
            v3.console.gotoxy(1, 3 + i);
            v3.console.write("\x1b[0m" + (lines[top + i] || "") + "\x1b[0m\x1b[K");
        }
        var shown = lines.length ? Math.min(100, Math.round((top + page) * 100 / lines.length)) : 100;
        this.footer("[N]ext  [P]rev  [T]ag  [Esc] list   " + shown + "%");
        var k = v3.console.readkey();
        if (!v3.session.online || k === "") return "";
        switch (k.toLowerCase()) {
            case "up": top = Math.max(0, top - 1); break;
            case "down": top = Math.min(last, top + 1); break;
            case "pgdn": case " ": top = Math.min(last, top + page); break;
            case "pgup": top = Math.max(0, top - page); break;
            case "home": top = 0; break;
            case "end": top = last; break;
            case "n": return "next";
            case "p": return "prev";
            case "t": this.toggleTag(); break;
            case "esc": case "q": return "list";
        }
    }
};

// open shows entries starting at index, moving to the next or previous
// art or text on N and P (passing over entries kindOf can't place), and
// returns false when the caller has gone.
ArtView.prototype.open = function (index) {
    var ask = true;
    for (;;) {
        var r = this.show(index, ask);
        ask = false;
        if (r === "") return false;
        var dir = r === "next" ? 1 : r === "prev" ? -1 : 0;
        if (dir === 0) {
            this.sel = index;
            v3.console.clear();
            return true;
        }
        var next = index + dir;
        while (next >= 0 && next < this.entries.length && this.kindOfEntry(this.entries[next]) !== "text") next += dir;
        if (next < 0 || next >= this.entries.length) {
            this.status = dir > 0 ? "That's the last piece" : "That's the first piece";
        } else {
            index = next;
        }
    }
};

ArtView.prototype.main = function () {
    v3.console.clear();
    var list = this.area ? v3.file.members(this.area.id, this.name, SAMPLE_BYTES) : null;
    if (list === null) {
        v3.console.writeln(COLORS.status + this.name + ": can't be opened." + "\x1b[0m");
        v3.console.pause();
        return;
    }
    for (var i = 0; i < list.length; i++) {
        var e = list[i];
        if (e.isDir) continue;
        // kindOf needs no more than the sample, so N/P and the list's
        // colours need no readMember for entries that have one.
        if ("sample" in e) {
            this.kinds[e.name] = e.sample === null ? "unreadable" : kindOf(e.name, e.sample);
            delete e.sample;
        }
        this.entries.push(e);
    }
    if (!this.entries.length) {
        v3.console.writeln(COLORS.status + this.name + " is empty." + "\x1b[0m");
        v3.console.pause();
        return;
    }
    for (;;) {
        this.drawList();
        var k = v3.console.readkey();
        if (!v3.session.online || k === "") return;
        var n = this.entries.length;
        switch (k.toLowerCase()) {
            case "up": this.sel = Math.max(0, this.sel - 1); break;
            case "down": this.sel = Math.min(n - 1, this.sel + 1); break;
            case "pgup": this.sel = Math.max(0, this.sel - this.rows()); break;
            case "pgdn": this.sel = Math.min(n - 1, this.sel + this.rows()); break;
            case "home": this.sel = 0; break;
            case "end": this.sel = n - 1; break;
            case "t": this.toggleTag(); break;
            case "\r":
                if (!this.open(this.sel)) return;
                break;
            case "esc": case "q":
                v3.console.write("\x1b[0m");
                v3.console.clear();
                return;
        }
    }
};

new ArtView(v3.args[v3.args.length - 2], v3.args[v3.args.length - 1]).main();
