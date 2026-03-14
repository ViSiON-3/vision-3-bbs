// userstats.js — Top Callers / Top Posters
//
// Displays leaderboards from the user database.
//
// Configure in doors.json:
//   { "name": "USERSTATS", "type": "v3_script", "script": "userstats.js",
//     "working_directory": "scripts/examples" }

var TOP_COUNT = 10;

function sortBy(arr, field) {
    // Simple bubble sort (goja supports Array methods but this is explicit).
    var sorted = arr.slice();
    for (var i = 0; i < sorted.length; i++) {
        for (var j = i + 1; j < sorted.length; j++) {
            if (sorted[j][field] > sorted[i][field]) {
                var tmp = sorted[i];
                sorted[i] = sorted[j];
                sorted[j] = tmp;
            }
        }
    }
    return sorted;
}

function displayTable(title, users, field, label) {
    v3.console.println("|11" + title);
    v3.console.println("|09" + v3.util.padRight("", 50, "-"));
    v3.console.println("|07" + v3.util.padRight("#", 4) + v3.util.padRight("Handle", 22) + v3.util.padLeft(label, 12));
    v3.console.println("|09" + v3.util.padRight("", 50, "-"));

    var count = Math.min(users.length, TOP_COUNT);
    for (var i = 0; i < count; i++) {
        var u = users[i];
        if (u[field] === 0) continue;
        var rank = v3.util.padRight((i + 1) + ".", 4);
        var handle = v3.util.padRight(u.handle, 22);
        var value = v3.util.padLeft("" + u[field], 12);
        v3.console.println("|15" + rank + "|07" + handle + "|14" + value);
    }
    v3.console.println("");
}

v3.console.clear();
v3.console.println("|09=== |15User Statistics |09===");
v3.console.println("");

var allUsers = v3.users.list();

// Top Callers
var byTimesCalled = sortBy(allUsers, "timesCalled");
displayTable("Top Callers", byTimesCalled, "timesCalled", "Calls");

// Top Posters
var byMessagesPosted = sortBy(allUsers, "messagesPosted");
displayTable("Top Posters", byMessagesPosted, "messagesPosted", "Messages");

// Top Uploaders
var byUploads = sortBy(allUsers, "uploads");
displayTable("Top Uploaders", byUploads, "uploads", "Uploads");

v3.console.pause();
