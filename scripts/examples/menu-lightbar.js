// menu-lightbar.js — Vision/3 VPL Script Example
//
// Draws any menu as a lightbar list of the commands in v3.menu.items. Up and
// Down move the bar, Enter picks the command under it, and a command's key
// picks it at once. Name the door in a menu's .MNU as "SCRIPT": "LIGHTBAR".
// Configure in doors.json as:
//   { "code": "LIGHTBAR", "name": "Lightbar Menu", "type": "v3_script",
//     "script": "menu-lightbar.js", "working_directory": "scripts/examples" }
// An optional first argument ("args": ["Main Menu"]) is shown as the title.

// v3.menu exists only for a menu's SCRIPT, so check the kind first.
if (v3.prompt.kind !== "menu" || v3.menu.items.length === 0) {
    exit(); // no answer: the menu shows its own screen and prompt
}
var items = v3.menu.items;

var title = v3.args.length > 0 ? v3.args[0] : "Menu";
var rows = Math.max(1, v3.console.height - 4); // list rows that fit
var sel = 0;
var top = 0;

function drawRow(i) {
    var it = items[i];
    var text = v3.util.padRight((" " + it.key + "  " + it.label).substring(0, 34), 34);
    v3.console.gotoxy(3, 3 + i - top);
    v3.console.print(i === sel ? "|B4|15" + text : "|B0|07" + text);
    v3.console.print("|B0|07");
}

function drawAll() {
    v3.console.clear();
    v3.console.println("|11 " + title + "  |08Up/Down, Enter or a key");
    for (var i = top; i < items.length && i < top + rows; i++) {
        drawRow(i);
    }
}

function choose(i) {
    v3.console.gotoxy(1, 3 + Math.min(rows, items.length - top));
    v3.prompt.answer(items[i].key);
}

drawAll();
for (;;) {
    var k = v3.console.readkey();
    var old = sel;
    if (k === "UP" && sel > 0) sel--;
    else if (k === "DOWN" && sel < items.length - 1) sel++;
    else if (k === "HOME") sel = 0;
    else if (k === "END") sel = items.length - 1;
    else if (k === "\r") { choose(sel); break; }
    else if (k.length === 1) {
        var hit = -1;
        for (var j = 0; j < items.length; j++) {
            if (items[j].key.toUpperCase() === k.toUpperCase()) { hit = j; break; }
        }
        if (hit >= 0) { choose(hit); break; }
    }
    if (sel === old) continue;
    if (sel < top || sel >= top + rows) { // scrolled off: redraw the page
        top = sel < top ? sel : sel - rows + 1;
        drawAll();
    } else {
        drawRow(old);
        drawRow(sel);
    }
}
