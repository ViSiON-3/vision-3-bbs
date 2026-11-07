// Archive viewer: what V opens on a ZIP in the file lists and VIEW_FILE, in
// place of ZipLab's contents list, when config.json's archiveViewerDoor
// names the ARTVIEW door (doors.json, hidden).
//
// The BBS passes the archive's area tag and filename as the last two
// arguments. A lightbar lists the archive's entries with each piece's SAUCE
// title and artist. Enter shows a piece: ANSI art is drawn as ANSI.SYS
// would (v3.ansi.render) and shown a screen at a time, text likewise.
// Entries are read in memory with v3.file.readMember, which needs the
// area's download access; nothing is extracted to disk.
//
// In the list: Up/Down, PgUp/PgDn, Home/End, Enter to view, T to tag the
// archive for download, Esc or Q to leave. Viewing a piece: Up/Down a line,
// PgUp/PgDn (or Space) a screen, Home/End, N/P the next or previous piece,
// T to tag, Esc or Q back to the list.
//
// To change the look, edit COLORS below.

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

// Entries the viewer can show: art and text by name, checked for binary
// bytes when read.
var VIEWABLE = /\.(ans|asc|ice|diz|nfo|txt|doc|lit|1st|me|now|ad|log|mem|lst|faq)$/i;

function sizeText(n) {
    if (n >= 1048576) return (n / 1048576).toFixed(1) + "M";
    if (n >= 1024) return Math.round(n / 1024) + "K";
    return n + "b";
}

function pad(s, n) {
    s = String(s);
    return s.length >= n ? s.slice(0, n) : s + new Array(n - s.length + 1).join(" ");
}

// looksBinary reports whether data, up to its Ctrl-Z (where a SAUCE record
// and its NUL padding start), looks like something other than text.
function looksBinary(data) {
    var end = data.indexOf("\x1a");
    var sample = data.slice(0, end >= 0 ? Math.min(end, 4096) : 4096);
    var low = 0;
    for (var i = 0; i < sample.length; i++) {
        var c = sample.charCodeAt(i);
        if (c === 0) return true;
        if (c < 8 && c !== 7) low++;
    }
    return low > 8;
}

function ArtView(tag, name) {
    this.area = v3.file.area(tag);
    this.name = name;
    this.entries = [];
    this.sauce = {};
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

// rows is how many rows fit between the two-row header and the footer.
ArtView.prototype.rows = function () {
    return this.height() - 3;
};

ArtView.prototype.viewable = function (e) {
    return VIEWABLE.test(e.name);
};

// sauceOf reads and remembers an entry's SAUCE record (null for none).
ArtView.prototype.sauceOf = function (e) {
    if (!(e.name in this.sauce)) {
        var data = this.viewable(e) ? v3.file.readMember(this.area.id, this.name, e.name) : null;
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
// or "list", or "" when the caller has gone.
ArtView.prototype.show = function (index) {
    var e = this.entries[index];
    var data = v3.file.readMember(this.area.id, this.name, e.name);
    if (data === null || looksBinary(data)) {
        this.status = e.name + ": can't be shown";
        return "list";
    }
    var lines = v3.ansi.render(data, { width: Math.min(this.width(), 80), rows: 4000 });
    var s = this.sauceOf(e);
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
// viewable one on N and P, and returns false when the caller has gone.
ArtView.prototype.open = function (index) {
    for (;;) {
        var r = this.show(index);
        if (r === "") return false;
        var dir = r === "next" ? 1 : r === "prev" ? -1 : 0;
        if (dir === 0) {
            this.sel = index;
            v3.console.clear();
            return true;
        }
        var next = index + dir;
        while (next >= 0 && next < this.entries.length && !this.viewable(this.entries[next])) next += dir;
        if (next < 0 || next >= this.entries.length) {
            this.status = dir > 0 ? "That's the last piece" : "That's the first piece";
        } else {
            index = next;
        }
    }
};

ArtView.prototype.main = function () {
    v3.console.clear();
    var list = this.area ? v3.file.members(this.area.id, this.name) : null;
    if (list === null) {
        v3.console.writeln(COLORS.status + this.name + ": can't be opened." + "\x1b[0m");
        v3.console.pause();
        return;
    }
    for (var i = 0; i < list.length; i++) {
        if (!list[i].isDir) this.entries.push(list[i]);
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
                if (!this.viewable(this.entries[this.sel])) {
                    this.status = this.entries[this.sel].name + ": not art or text";
                } else if (!this.open(this.sel)) {
                    return;
                }
                break;
            case "esc": case "q":
                v3.console.write("\x1b[0m");
                v3.console.clear();
                return;
        }
    }
};

new ArtView(v3.args[v3.args.length - 2], v3.args[v3.args.length - 1]).main();
