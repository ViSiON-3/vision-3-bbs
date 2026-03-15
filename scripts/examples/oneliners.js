// oneliners.js — Classic BBS Oneliners Wall
//
// Displays recent oneliners and lets users add new ones.
// Uses v3.data for persistent storage.
//
// Configure in doors.json:
//   { "name": "ONELINERS", "type": "v3_script", "script": "oneliners.js",
//     "working_directory": "scripts/examples" }

var MAX_LINES = 15;
var MAX_LENGTH = 70;

function loadOneliners() {
    var lines = v3.data.get("lines");
    if (!lines) return [];
    return lines;
}

function saveOneliners(lines) {
    v3.data.set("lines", lines);
}

function displayOneliners(lines) {
    v3.console.clear();
    v3.ansi.displayRaw("ONELINER.TOP.ANS");
    v3.console.println("");

    if (lines.length === 0) {
        v3.console.println("|08  No oneliners yet. Be the first!");
    } else {
        for (var i = 0; i < lines.length; i++) {
            var entry = lines[i];
            v3.console.println("|03" + v3.util.padRight(entry.handle, 16) + "|07" + entry.text);
        }
    }

    v3.console.println("");
    v3.console.println("|09" + v3.util.padRight("", 70, "="));
}

var lines = loadOneliners();
displayOneliners(lines);

v3.console.println("");
if (v3.console.yesno("|07Add an oneliner")) {
    v3.console.print("|07> |15");
    var text = v3.console.getstr(MAX_LENGTH);
    if (text && text.length > 0) {
        lines.push({
            handle: v3.user.handle,
            text: text,
            time: v3.util.time()
        });
        // Keep only the most recent entries.
        while (lines.length > MAX_LINES) {
            lines.shift();
        }
        saveOneliners(lines);
        v3.console.println("|10Added!");
    }
}

v3.console.pause();
