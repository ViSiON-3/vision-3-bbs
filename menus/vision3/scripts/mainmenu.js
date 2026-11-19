// mainmenu.js: the ViSiON/3 theme's main menu.
//
// A theme script: MAIN.MNU names it with "SCRIPT": "mainmenu.js". The BBS
// hands it the menu in v3.menu (items the caller may use, every shortcut)
// and it answers with v3.prompt.answer(key). Only labelled commands are
// shown; every shortcut in MAIN.CFG still works when typed.
//
// The logo is data/vision3-logo.png, drawn as sixel when v3.gfx.sixel says
// the terminal draws it, with data/vision3-palette.png as the one palette
// for the logo and the cube. Without sixel the logo is text. On a screen
// smaller than 80x24 the script answers nothing and the menu shows its own
// screen and prompt.
var items = v3.menu.items || [], shortcuts = v3.menu.shortcuts || [];
var w = v3.console.width, h = v3.console.height;
var columns = 3, rows = Math.ceil(items.length / columns), selected = 0;
var start = 12, cell = Math.floor((Math.min(w, 80) - 4) / columns);
// Add "classic" after the script name in SCRIPT ("mainmenu.js classic") for
// the 16-colour treatment on terminals without 24-bit colour.
var truecolor = v3.args.indexOf('classic') < 0;
var tick = 0;
var startedAt = Date.now();
var animationTime = 0, lastCubeFrame = -1, lastLogoFrame = -1, lastMenuTick = -1, lastFrameGlint = -1;
var logoCanvas = null;
var cubeCanvas = null, cubePosition = "";
var cubeSeed = Math.random() * Math.PI * 2;
// Shapes hold for a random interval, then blend over 1.8 seconds.
var cubeMorph = {from: 0, to: 0, begin: 0, next: 3 + Math.random() * 4};
var emberSources = [], emberReach = [];

function at(x, y, text) { v3.console.gotoxy(x, y); v3.console.print(text); }
function rgb(r, g, b) { return '\x1b[38;2;' + r + ';' + g + ';' + b + 'm'; }
function rawAt(x, y, text) { v3.console.gotoxy(x, y); v3.console.write(text); }
// Light only visible metal, leaving the black background untouched.
function paintLight(canvas, x, y, radius, red, green, blue, intensity) {
  for (var dy = -radius; dy <= radius; dy++) {
    for (var dx = -radius; dx <= radius; dx++) {
      var distance = Math.sqrt(dx * dx + dy * dy) / (radius + 1);
      if (distance >= 1) continue;
      var old = canvas.getPixel(x + dx, y + dy);
      if (!old || Math.max(old[0], old[1], old[2]) < 28) continue;
      var light = intensity * Math.pow(1 - distance, 2);
      canvas.setPixel(x + dx, y + dy,
        Math.min(255, Math.round(old[0] + red * light)),
        Math.min(255, Math.round(old[1] + green * light)),
        Math.min(255, Math.round(old[2] + blue * light)), 255);
    }
  }
}
// Find hot letter pixels once, before animation; sparks originate on the
// actual artwork, rather than from arbitrary points below it.
function findEmberSources(canvas) {
  var points = [];
  for (var y = 12; y < 150; y += 4) {
    for (var x = 4; x < 300; x += 4) {
      var p = canvas.getPixel(x, y);
      if (p && p[0] > 150 && p[1] > 55 && p[0] > p[2] * 1.5) points.push([x, y]);
    }
  }
  // The metal is hottest at the far-left V and cools along the word, so
  // embers come off thickest there and thin out to the right. emberReach
  // holds each source's running share of that heat.
  var left = 1e9, total = 0;
  for (var i = 0; i < points.length; i++) left = Math.min(left, points[i][0]);
  emberReach = [];
  for (var j = 0; j < points.length; j++) {
    total += Math.exp(-(points[j][0] - left) / 65);
    emberReach.push(total);
  }
  return points;
}
// The source that share u (0 to 1) of the heat reaches.
function emberSource(u) {
  var want = u * emberReach[emberReach.length - 1], lo = 0, hi = emberReach.length - 1;
  while (lo < hi) {
    var mid = (lo + hi) >> 1;
    if (emberReach[mid] < want) lo = mid + 1; else hi = mid;
  }
  return emberSources[lo];
}
// Embers rise from hot metal on the convection above it.
// Each ember has its own stamina, cooling curve and burn-out, so some wink
// out low while others ride an updraft to the top, flaring and guttering as
// they cross warmer and cooler air; a slow shared current carries
// neighbouring embers the same way.
function paintEmbers(canvas, time) {
  function random(n) { var v = Math.sin(n * 127.1 + 311.7) * 43758.5453123; return v - Math.floor(v); }
  // The current at a point: -1 pushes left, 1 right. It meanders with
  // height and drifts over time, so a plume leans and breaks up.
  function current(x, y) { return Math.sin(x * 0.021 + Math.sin(y * 0.047 - time * 0.7) * 1.6 + time * 0.33); }
  for (var i = 0; i < 64; i++) {
    var seed = i * 7 + 1, phase = random(seed), kind = random(seed + 1);
    var rate = kind < 0.45 ? 0.23 : kind < 0.86 ? 0.13 : 0.08;
    // Stamina is how much of the full flight this ember lasts; a spent
    // ember is relit at a source straight away, so the count holds.
    var stamina = 0.3 + 0.7 * Math.pow(random(seed + 5), 0.8);
    var burn = (time * rate / stamina + phase) % 1;
    var life = burn * stamina;
    var climb = 1 - Math.exp(-(kind < 0.45 ? 4.0 : 1.7) * life);
    var rise = kind < 0.45 ? 24 : kind < 0.86 ? 88 : 120;
    if (!emberSources.length) continue;
    // Each relight picks a new source, weighted to the hottest metal.
    var source = emberSource(random(seed + 3 + Math.floor(time * rate / stamina + phase) * 17.3));
    var y = Math.round(source[1] - climb * rise - life * 8);
    var sway = Math.sin(life * 6.28 + phase * 6.28) * (2 + 6 * climb);
    var drift = current(source[0], y);
    var x = Math.round(source[0] + sway + drift * (4 + 22 * climb));
    // Lit quickly; the burn-out is a snap for some and a long gutter for others.
    var fade = Math.min(1, burn / 0.05) * Math.min(1, (1 - burn) / (0.05 + 0.3 * random(seed + 8)));
    var temp = Math.pow(1 - burn, 0.6 + 1.6 * random(seed + 6));
    // Gusts are each ember's own; the draft is the shared current, hottest
    // where the air rises straight.
    var gust = 0.5 + 0.5 * Math.sin(time * (2.1 + 3.7 * random(seed + 7)) + phase * 40 + y * 0.09);
    var draft = 1 - Math.abs(drift);
    var intensity = fade * (0.6 + 0.4 * temp) * (0.65 + 0.8 * (0.6 * gust + 0.4 * draft));
    var radius = kind < 0.45 ? 1 : kind < 0.86 ? 2 : 3;
    var red = Math.round(150 + 105 * Math.min(1, temp * 3));
    var green = Math.round(45 + 177 * temp);
    var blue = Math.round(8 + 135 * Math.pow(temp, 2));
    if (i % 2 === 0 && intensity > 0.12) paintLight(canvas, x, y, 4, 105, 35, 4, intensity * 0.55);
    for (var dy = -radius; dy <= radius; dy++) {
      for (var dx = -radius; dx <= radius; dx++) {
        var d = Math.sqrt(dx * dx + dy * dy) / (radius + 0.4);
        if (d >= 1) continue;
        var old = canvas.getPixel(x + dx, y + dy);
        if (!old) continue;
        var light = intensity * Math.pow(1 - d, 2);
        canvas.setPixel(x + dx, y + dy,
          Math.min(255, Math.round(old[0] + red * light)),
          Math.min(255, Math.round(old[1] + green * light)),
          Math.min(255, Math.round(old[2] + blue * light)), 255);
      }
    }
  }
}
// Brief tack-weld bursts: a cold arc and orange spatter. A cropped, uneven
// star avoids a circular flashlight halo.
function paintWeld(canvas, tick, step) {
  var strengths = [1, 0.12, 0.9, 0.15, 0.65];
  var power = step < strengths.length ? strengths[step] : 0;
  // Strike centres sit on the silhouette or inner edges of the finished N,
  // slash and 3 (coordinates are for the 480x160 logo).
  var strike = Math.floor(tick / 48), spots = [[304, 20], [327, 56], [355, 20], [401, 52], [425, 96], [449, 112], [450, 137], [461, 74]];
  var center = spots[strike % spots.length], angle = strike * 1.71, cut = strike % 2 ? -1 : 1;
  if (power > 0) paintLight(canvas, center[0], center[1], 16, 65, 105, 175, power * 0.65);
  for (var y = -11; y <= 11; y++) {
    for (var x = -15; x <= 15; x++) {
      var distance = Math.sqrt(x*x + y*y*1.9);
      if (distance > 14) continue;
      var theta = Math.atan2(y, x) - angle;
      var core = Math.max(0, 1 - distance / 3.2);
      var spike = Math.pow(Math.max(0, Math.cos(theta * 4)), 20) * Math.max(0, 1 - distance / 14);
      var occlusion = x * cut < -3 ? 0.3 : 1;
      var light = power * occlusion * Math.min(1, core + spike * 0.65);
      if (light < 0.025) continue;
      var px = center[0] + x, py = center[1] + y, old = canvas.getPixel(px, py);
      if (!old) continue;
      canvas.setPixel(px, py, Math.min(255, Math.round(old[0] + 205*light)),
        Math.min(255, Math.round(old[1] + 225*light)), Math.min(255, Math.round(old[2] + 255*light)), 255);
    }
  }
  // Spatter keeps moving after the arc goes out. Each particle has its own
  // launch time, velocity, cooling and short fading motion trail.
  for (var i = 0; i < 20; i++) {
    var age = step - (i % 4);
    if (age < 0 || age > 10) continue;
    var spread = (i % 7 - 3) * 0.24;
    var direction = strike % 2 ? -1 : 1;
    var vx = direction * (1.7 + (i % 5) * 0.55) + spread;
    var vy = -1.7 + (i % 6) * 0.57;
    for (var trail = 2; trail >= 0; trail--) {
      var t = age - trail * 0.65;
      if (t < 0) continue;
      var sx = Math.round(center[0] + vx * t);
      var sy = Math.round(center[1] + vy * t + 0.22 * t * t);
      var glow = Math.max(0, 1 - age / 11) * (1 - trail * 0.25);
      var old = canvas.getPixel(sx, sy);
      if (!old) continue;
      canvas.setPixel(sx, sy, Math.min(255, Math.round(old[0] + 255 * glow)),
        Math.min(255, Math.round(old[1] + (65 + 145 * glow) * glow)),
        Math.min(255, Math.round(old[2] + 50 * glow * glow)), 255);
    }
  }
}
// A rare, brief specular "ting" on the edge of the finished 3.
function paintGlint(canvas, time) {
  var phase = (time + 33) % 55;
  if (phase >= 0.8) return;
  var bright = Math.sin(Math.PI * phase / 0.8), cx = 450, cy = 137;
  for (var d = -7; d <= 7; d++) {
    var light = bright * Math.max(0, 1 - Math.abs(d) / 8);
    var points = [[cx + d, cy], [cx, cy + Math.round(d * 0.6)]];
    for (var i = 0; i < points.length; i++) {
      var p = points[i], old = canvas.getPixel(p[0], p[1]);
      if (!old) continue;
      canvas.setPixel(p[0], p[1], Math.min(255, Math.round(old[0] + 235 * light)),
        Math.min(255, Math.round(old[1] + 240 * light)), Math.min(255, Math.round(old[2] + 255 * light)), 255);
    }
  }
}
function center(y, text) {
  var width = v3.util.stripPipe(text).length;
  at(Math.max(1, Math.floor((Math.min(w, 80) - width) / 2) + 1), y, text);
}
// Render a translucent pixel cube in the reserved five-cell gutter. Faces
// are alpha-composited back-to-front; the twelve edges rotate in 3D. Both
// canvases use the same sixel palette so updating one cannot recolour the other.
function drawCube(i, erase) {
  if (i < 0 || i >= items.length) return;
  var col = Math.floor(i / rows), row = i % rows, height = Math.min(3, rows);
  var top = Math.max(start, Math.min(start + row - 1, start + rows - height));
  var left = 3 + col * cell;
  if (erase) {
    if (cubeCanvas) {
      for (var n = 0; n < height; n++) rawAt(left, top + n, '\x1b[0;40m     \x1b[0m');
      cubePosition = '';
    }
    return;
  }
  if (!v3.gfx || !v3.gfx.sixel || !logoCanvas) return;
  try {
    if (!cubeCanvas) cubeCanvas = v3.gfx.canvas(40, height * 16, 'vision3-palette.png');
    if (animationTime >= cubeMorph.next) {
      cubeMorph.from = cubeMorph.to;
      cubeMorph.to = (cubeMorph.from + 1 + Math.floor(Math.random() * 2)) % 3;
      cubeMorph.begin = animationTime;
      cubeMorph.next = animationTime + 1.8 + 4 + Math.random() * 6;
    }
    var morph = Math.max(0, Math.min(1, (animationTime - cubeMorph.begin) / 1.8));
    morph = morph * morph * (3 - 2 * morph);
    var diamondWeight = (cubeMorph.from === 2 ? 1-morph : 0) + (cubeMorph.to === 2 ? morph : 0);
    var cubeWeight = (cubeMorph.from === 0 ? 1-morph : 0) + (cubeMorph.to === 0 ? morph : 0);
    // A different seed for each visit gently varies all three axes without
    // abrupt random jumps in the cube's motion.
    var pw = 40, ph = height * 16;
    var cubeClock = animationTime * 6.25;
    var theta = cubeClock*(0.075+0.025*Math.sin(cubeSeed)) + 0.42*Math.sin(cubeClock*0.027 + cubeSeed);
    var cs = Math.cos(theta), sn = Math.sin(theta);
    // The canvas is kept inside the frame; the cube still belongs to the
    // selected row, including at the first and last entries in a column.
    var centerY = (start + row - top) * 16 + 8;
    var tilt = 0.40 + cubeClock*(0.035+0.012*Math.cos(cubeSeed*1.3)) + 0.34*Math.sin(cubeClock*0.021 + cubeSeed*1.7);
    var roll = cubeClock*(0.025+0.010*Math.sin(cubeSeed*1.9)) + 0.24*Math.sin(cubeClock*0.033 + cubeSeed*0.7);
    var ct = Math.cos(tilt), st = Math.sin(tilt), cr = Math.cos(roll), sr = Math.sin(roll), pts = [];
    var scale = Math.min(pw, ph) * 0.19;
    function project(x,y,z) {
      var rx = x * cs + z * sn, rz = z * cs - x * sn;
      var ry = y * ct - rz * st; rz = y * st + rz * ct;
      var rolledX = rx*cr - ry*sr, rolledY = rx*sr + ry*cr;
      var perspective = 3.8 / (4.8 + rz);
      return {x: pw/2 + rolledX*perspective*scale, y: centerY - rolledY*perspective*scale, z: rz};
    }
    function vertex(shape, v) {
      if (shape === 0) return [(v & 1) ? 1 : -1, (v & 2) ? 1 : -1, (v & 4) ? 1 : -1];
      if (shape === 1) return v < 4 ? [(v & 1) ? 1 : -1, -1, (v & 2) ? 1 : -1] : [0, 1.25, 0];
      var diamond = [[-1.35,0,0],[0,0,-1.35],[0,0,1.35],[1.35,0,0],[0,1.6,0],[0,1.6,0],[0,-1.6,0],[0,-1.6,0]];
      return diamond[v];
    }
    for (var v = 0; v < 8; v++) {
      var a = vertex(cubeMorph.from,v), b = vertex(cubeMorph.to,v);
      pts.push(project(a[0]+(b[0]-a[0])*morph, a[1]+(b[1]-a[1])*morph, a[2]+(b[2]-a[2])*morph));
    }
    // At the first/last row the selected cell is near the canvas boundary.
    // Shift only enough to fit the complete projected cube; the frame never
    // covers it, and it stays as close to the selected row as possible.
    var minY=ph, maxY=0;
    for (var p=0; p<pts.length; p++) { minY=Math.min(minY,pts[p].y); maxY=Math.max(maxY,pts[p].y); }
    var moveY = minY < 1 ? 1-minY : maxY > ph-2 ? ph-2-maxY : 0;
    if (moveY) for (var m=0; m<pts.length; m++) pts[m].y += moveY;
    cubeCanvas.clear(0,0,0);
    // Tiny points circulate inside the cube's own coordinate space. They are
    // drawn behind the translucent faces, so the glass tints the embers.
    for (var spark=0; spark<12; spark++) {
      var phase = cubeSeed + spark*2.399, age = cubeClock*0.075 + phase;
      var radius = 0.30 + 0.32*cubeWeight + 0.12*diamondWeight;
      var sp = project(radius*Math.sin(age*0.73+phase), radius*Math.sin(age*0.91+phase*0.7), radius*Math.cos(age*0.61+phase));
      var flicker = 0.5+0.5*Math.sin(age*2.3+phase);
      var sx=Math.round(sp.x), sy=Math.round(sp.y+moveY);
      cubeCanvas.setPixel(sx,sy,255,Math.round(100+135*flicker),Math.round(15+100*flicker),255);
      if (flicker > 0.65) cubeCanvas.setPixel(sx,sy+1,235,75,10,255);
    }
    var faces = [[0,1,3,2],[4,5,7,6],[0,1,5,4],[2,3,7,6],[0,2,6,4],[1,3,7,5]];
    faces.sort(function(a,b) { return (pts[b[0]].z+pts[b[1]].z+pts[b[2]].z+pts[b[3]].z) - (pts[a[0]].z+pts[a[1]].z+pts[a[2]].z+pts[a[3]].z); });
    function edge(a,b,x,y) { return (x-a.x)*(b.y-a.y) - (y-a.y)*(b.x-a.x); }
    function triangle(a,b,c,shade) {
      var area = edge(a,b,c.x,c.y); if (Math.abs(area) < 0.1) return;
      var x0=Math.max(0,Math.floor(Math.min(a.x,b.x,c.x))), x1=Math.min(pw-1,Math.ceil(Math.max(a.x,b.x,c.x)));
      var y0=Math.max(0,Math.floor(Math.min(a.y,b.y,c.y))), y1=Math.min(ph-1,Math.ceil(Math.max(a.y,b.y,c.y)));
      for (var yy=y0; yy<=y1; yy++) for (var xx=x0; xx<=x1; xx++) {
        var e1=edge(a,b,xx+0.5,yy+0.5), e2=edge(b,c,xx+0.5,yy+0.5), e3=edge(c,a,xx+0.5,yy+0.5);
        if ((area > 0 && (e1<0 || e2<0 || e3<0)) || (area < 0 && (e1>0 || e2>0 || e3>0))) continue;
        var old=cubeCanvas.getPixel(xx,yy), alpha=0.24;
        cubeCanvas.setPixel(xx,yy,Math.round(old[0]*(1-alpha)+200*shade*alpha),
          Math.round(old[1]*(1-alpha)+70*shade*alpha),Math.round(old[2]*(1-alpha)+12*shade*alpha),255);
      }
    }
    for (var f=0; f<faces.length; f++) {
      var face=faces[f], shade=0.75+(f/Math.max(1,faces.length-1))*0.55;
      triangle(pts[face[0]],pts[face[1]],pts[face[2]],shade);
      triangle(pts[face[0]],pts[face[2]],pts[face[3]],shade);
    }
    var edges=[[0,1],[2,3],[4,5],[6,7],[0,2],[1,3],[4,6],[5,7],[0,4],[1,5],[2,6],[3,7]];
    if (diamondWeight > 0.1) edges = edges.concat([[0,6],[1,7],[2,4],[3,5]]);
    for (var e=0; e<edges.length; e++) {
      var a=pts[edges[e][0]], b=pts[edges[e][1]], steps=Math.max(1,Math.ceil(Math.max(Math.abs(b.x-a.x),Math.abs(b.y-a.y))));
      var shine=Math.max(0,Math.min(1,1-((a.z+b.z)*0.5+1.5)/3));
      for (var q=0; q<=steps; q++) cubeCanvas.setPixel(Math.round(a.x+(b.x-a.x)*q/steps),Math.round(a.y+(b.y-a.y)*q/steps),
        255,Math.round(110+125*shine),Math.round(25+120*shine),255);
    }
    var position=left+','+top;
    v3.console.gotoxy(left,top);
    if (position !== cubePosition) { cubeCanvas.present(); cubePosition=position; }
    else cubeCanvas.presentDelta();
  } catch (err) {
    // Menu selection continues even if graphics are unavailable.
    cubeCanvas = null; cubePosition = '';
  }
}
function item(i, active) {
  if (i < 0 || i >= items.length) return;
  var col = Math.floor(i / rows), row = i % rows;
  var key = String(items[i].key), label = String(items[i].label || items[i].activity || key);
  var width = cell - 6, text = ((active ? '>' : ' ') + key + ' ' + label).slice(0,width);
  while (text.length < width) text += ' ';
  var out = '\x1b[0;40m';
  for (var j = 0; j < text.length; j++) {
    if (!truecolor) out += active ? '\x1b[1;37m' : '\x1b[0;37m';
    else if (active) out += j <= key.length ? rgb(255,220,155) : rgb(235,230,215);
    else {
      var wave = (Math.sin(j*0.48-tick*0.14+row*0.65+col)+1)/2;
      out += j < key.length ? rgb(255,Math.round(115+115*wave),Math.round(20+140*wave)) :
        rgb(Math.round(175+65*wave),Math.round(160+40*wave),Math.round(145-55*wave));
    }
    out += text.charAt(j);
  }
  rawAt(3 + col*cell + 5, start+row, out+'\x1b[0m');
}
function titleFrame(frame) {
  var left = 1, right = 79, top = 11, bottom = start + rows;
  var width = right - left + 1, side = bottom - top - 1;
  var perimeter = 2 * width + 2 * side;
  // One lap followed by about thirty seconds of rest.
  var lap = frame % (Math.ceil(perimeter / 2) + 188);
  var glint = lap * 2 < perimeter ? lap * 2 : -1;
  var title = ' M A I N   M E N U ', titleStart = Math.floor((width - title.length) / 2);
  function shade(index, x, letter) {
    if (!truecolor) return letter ? '\x1b[1;33m' : '\x1b[0;37m';
    var edge = Math.min(1, Math.max(0, (x - left) / (right - left)));
    var pulse = (Math.sin(frame * 0.13 - x * 0.08) + 1) / 2;
    var gap = (glint - index + perimeter) % perimeter;
    var shine = glint >= 0 && gap < 7 ? 1 - gap / 7 : 0;
    var hot = 1 - edge;
    var r = letter ? 255 : 95 + 105*hot;
    var g = letter ? 155 + 70*pulse : 95 + 35*hot + 25*pulse;
    var b = letter ? 55 + 100*pulse : 110 - 55*hot;
    return rgb(Math.round(r+(255-r)*shine), Math.round(g+(245-g)*shine), Math.round(b+(215-b)*shine));
  }
  var out = '\x1b[0;40m';
  for (var i = 0; i < width; i++) {
    var isTitle = i >= titleStart && i < titleStart + title.length;
    var ch = isTitle ? title.charAt(i-titleStart) : i === 0 || i === width-1 ? '.' : '-';
    out += shade(i, left+i, isTitle) + ch;
  }
  rawAt(left, top, out + '\x1b[0m');
  for (var y = 0; y < side; y++) {
    rawAt(right, top+1+y, shade(width+y, right, false) + '|\x1b[0m');
    rawAt(left, top+1+y, shade(2*width+side+(side-1-y), left, false) + '|\x1b[0m');
  }
  out = '\x1b[0;40m';
  for (var j = 0; j < width; j++) {
    out += shade(width+side+(width-1-j), left+j, false) + (j === 0 ? '`' : j === width-1 ? "'" : '-');
  }
  rawAt(left, bottom, out + '\x1b[0m');
}
// across returns the item beside item i in the next column (dir 1) or the
// one before (dir -1) on the same row, wrapping round the columns and
// skipping a column with no item on that row.
function across(i, dir) {
  var cols = Math.ceil(items.length / rows), col = Math.floor(i / rows), row = i % rows;
  for (var step = 1; step < cols; step++) {
    var j = ((col + dir * step) % cols + cols) % cols * rows + row;
    if (j < items.length) return j;
  }
  return i;
}

function choose(key) {
  v3.console.print('|B0|07\x1b[?25h');
  v3.console.gotoxy(1, h);
  v3.prompt.answer(key);
}
function command(first) {
  if (first.length !== 1) return '';
  var key = first.toUpperCase();
  for (var n = 0; n < 32; n++) {
    var prefix = false, i, candidate;
    for (i = 0; i < items.length; i++) if (String(items[i].key).toUpperCase() === key) return key;
    for (i = 0; i < shortcuts.length; i++) {
      candidate = String(shortcuts[i]).toUpperCase();
      if (candidate === key) return key;
      if (candidate.indexOf(key) === 0) prefix = true;
    }
    if (!prefix) return '';
    // As at the board's own prompt, a key that starts a longer command
    // waits for the rest with no time limit.
    var next = v3.console.readkey();
    if (next === '\x10') return '^P';
    if (next.length !== 1 || next === '\r' || next === 'ESC') return '';
    key += next.toUpperCase();
  }
  return '';
}

// Defer to the normal menu on small screens or exceptionally large menus.
if (w >= 80 && h >= 24 && items.length && start + rows <= h - 2) {
  v3.console.print('\x1b[?25l');
  v3.console.clear();
  var picture = false;
  if (v3.gfx && v3.gfx.sixel) {
    try {
      logoCanvas = v3.gfx.canvas(480, 160, 'vision3-palette.png');
      logoCanvas.clear(0, 0, 0);
      logoCanvas.drawImage('vision3-logo.png', 0, 0);
      emberSources = findEmberSources(logoCanvas);
      paintEmbers(logoCanvas, 0);
      v3.console.gotoxy(11, 1);
      picture = logoCanvas.present();
    } catch (err) {
      logoCanvas = null;
      v3.console.gotoxy(11, 1);
      picture = v3.gfx.image('vision3-logo.png');
    }
  }
  if (!picture) {
    // FIGlet Graffiti lettering.
    var fallbackLogo = ["____   ____.__  _________.__________    _______        /\\ ________", "\\   \\ /   /|__|/   _____/|__\\_____  \\   \\      \\      / / \\_____  \\", " \\   Y   / |  |\\_____  \\ |  |/   |   \\  /   |   \\    / /    _(__  <", "  \\     /  |  |/        \\|  /    |    \\/    |    \\  / /    /       \\", "   \\___/   |__/_______  /|__\\_______  /\\____|__  / / /    /______  /", "                      \\/            \\/         \\/  \\/            \\/"];
    var logoWidth = 0;
    for (var lr = 0; lr < fallbackLogo.length; lr++) logoWidth = Math.max(logoWidth, fallbackLogo[lr].length);
    var logoLeft = Math.floor((80 - logoWidth) / 2) + 1;
    for (var ly = 0; ly < fallbackLogo.length; ly++) {
      var line = fallbackLogo[ly], art = '\x1b[0;40m';
      for (var lx = 0; lx < line.length; lx++) {
        if (truecolor) {
          var steel = Math.min(1, Math.max(0, (lx / logoWidth - 0.48) / 0.40));
          art += rgb(Math.round(255 - 75*steel), Math.round(157 + 20*steel), Math.round(33 + 150*steel));
        } else {
          art += lx < logoWidth / 2 ? '\x1b[1;33m' : '\x1b[0;37m';
        }
        art += line.charAt(lx);
      }
      rawAt(logoLeft, 2 + ly, art + '\x1b[0m');
    }
  }
  titleFrame(0);
  for (var i = 0; i < items.length; i++) item(i, false);
  center(h - 1, '|08ARROWS |07move  |08/  |14ENTER |07select  |08/  |07type a key to jump');
  item(selected, true);
  drawCube(selected, false);
  for (;;) {
    var key = v3.console.readkey(25);
    if (!key) {
      var elapsed = (Date.now() - startedAt) / 1000;
      animationTime = elapsed;
      tick = Math.floor(elapsed / 0.16);
      var cubeFrame = Math.floor(elapsed * 24);
      if (cubeFrame !== lastCubeFrame) {
        lastCubeFrame = cubeFrame;
        drawCube(selected, false);
      }
      if (truecolor) {
        var glintFrame = Math.floor(elapsed * 20);
        if (glintFrame !== lastFrameGlint) {
          lastFrameGlint = glintFrame;
          titleFrame(elapsed * 6.25);
        }
        if (tick !== lastMenuTick) {
          lastMenuTick = tick;
          // Repaint one row per tick, leaving the cube's gutter untouched.
          for (var c = 0; c < columns; c++) {
            var warm = c * rows + (tick % rows);
            if (warm !== selected) item(warm, false);
          }
          item(selected, true);
        }
        var weldStep = tick % 48 - 8;
        var logoFrame = Math.floor(elapsed * 8);
        if (logoCanvas && logoFrame !== lastLogoFrame) {
          lastLogoFrame = logoFrame;
          logoCanvas.clear(0, 0, 0);
          logoCanvas.drawImage('vision3-logo.png', 0, 0);
          logoCanvas.heat(elapsed, 0.04, true);
          paintEmbers(logoCanvas, elapsed);
          if (weldStep >= 0 && weldStep <= 14) paintWeld(logoCanvas, tick, weldStep);
          paintGlint(logoCanvas, elapsed);
          v3.console.gotoxy(11, 1);
          logoCanvas.presentDelta();
        }
      }
      continue;
    }
    // Ctrl+P goes back to the previous menu, as at the board's own prompt.
    if (key === '\x10') { choose('^P'); break; }
    var cmd = command(key);
    if (cmd) { choose(cmd); break; }
    var next = selected;
    if (key === 'UP') next = (selected + items.length - 1) % items.length;
    else if (key === 'DOWN') next = (selected + 1) % items.length;
    else if (key === 'LEFT') next = across(selected, -1);
    else if (key === 'RIGHT') next = across(selected, 1);
    else if (key === '\r' || key === ' ') { choose(items[selected].key); break; }
    if (next !== selected) { drawCube(selected, true); item(selected, false); selected = next; item(selected, true); drawCube(selected, false); }
  }
  v3.console.print('\x1b[?25h');
}
