(() => {
  const canvas = document.getElementById('game');
  const ctx = canvas.getContext('2d');
  const viewWidth = canvas.width;
  const viewHeight = canvas.height;

  const world = { width: 3400 };
  const groundY = viewHeight - 90;

  const physics = {
    gravity: 3000,
    maxFallSpeed: 1600,
  };

  const player = {
    x: 120,
    y: groundY - 200,
    width: 34,
    height: 46,
    smallHeight: 46,
    bigHeight: 70,
    vx: 0,
    vy: 0,
    groundAccel: 1800,
    airAccel: 900,
    drag: 3200,
    maxSpeed: 280,
    jumpVelocitySmall: -820,
    jumpVelocityBig: -940,
    onGround: false,
    isBig: false,
    mushrooms: 0,
    powerTimer: 0,
  };

  const camera = { x: 0 };

  const solids = buildTerrain();
  const mushrooms = buildMushrooms();
  const clouds = buildClouds();

  const input = { left: false, right: false };

  let lastTime = 0;
  let elapsed = 0;

  function buildTerrain() {
    const solidColor = '#2f7b32';
    const blocks = [];
    // Ground strip
    blocks.push({ x: 0, y: groundY, width: world.width, height: viewHeight - groundY, color: solidColor });
    // Platforms lowered to ease jumps
    blocks.push({ x: 280, y: groundY - 90, width: 220, height: 24, color: '#3f8f42' });
    blocks.push({ x: 720, y: groundY - 60, width: 180, height: 24, color: '#3f8f42' });
    blocks.push({ x: 1040, y: groundY - 125, width: 220, height: 24, color: '#3f8f42' });
    blocks.push({ x: 1420, y: groundY - 80, width: 200, height: 24, color: '#3f8f42' });
    blocks.push({ x: 1900, y: groundY - 110, width: 260, height: 24, color: '#3f8f42' });
    blocks.push({ x: 2320, y: groundY - 55, width: 180, height: 24, color: '#3f8f42' });
    blocks.push({ x: 2680, y: groundY - 95, width: 220, height: 24, color: '#3f8f42' });
    blocks.push({ x: 3050, y: groundY - 85, width: 180, height: 24, color: '#3f8f42' });
    // Pipes / obstacles
    blocks.push({ x: 600, y: groundY - 90, width: 70, height: 90, color: '#2d6931' });
    blocks.push({ x: 1520, y: groundY - 130, width: 80, height: 130, color: '#2d6931' });
    blocks.push({ x: 2200, y: groundY - 150, width: 90, height: 150, color: '#2d6931' });
    return blocks;
  }

  function buildMushrooms() {
    const spots = [
      { x: 360, surface: groundY - 90 },
      { x: 890, surface: groundY },
      { x: 1220, surface: groundY - 125 },
      { x: 1880, surface: groundY - 110 },
      { x: 2460, surface: groundY },
      { x: 2970, surface: groundY - 85 },
    ];
    return spots.map((spot) => {
      const size = 28;
      return {
        x: spot.x,
        y: spot.surface - size,
        width: size,
        height: size,
        collected: false,
        seed: Math.random() * Math.PI * 2,
      };
    });
  }

  function buildClouds() {
    const list = [];
    for (let i = 0; i < 8; i += 1) {
      list.push({
        x: Math.random() * world.width,
        y: 60 + Math.random() * 140,
        scale: 0.6 + Math.random() * 0.8,
        speed: 10 + Math.random() * 20,
      });
    }
    return list;
  }

  function rectsIntersect(a, b) {
    return (
      a.x < b.x + b.width &&
      a.x + a.width > b.x &&
      a.y < b.y + b.height &&
      a.y + a.height > b.y
    );
  }

  function clamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function attemptJump() {
    if (player.onGround) {
      const jumpVelocity = player.isBig ? player.jumpVelocityBig : player.jumpVelocitySmall;
      player.vy = jumpVelocity;
      player.onGround = false;
    }
  }

  function growPlayer() {
    if (!player.isBig) {
      const bottom = player.y + player.height;
      player.height = player.bigHeight;
      player.y = bottom - player.height;
      player.isBig = true;
    }
    player.powerTimer = 2.1;
  }

  function resolveAxis(axis) {
    for (const solid of solids) {
      if (!rectsIntersect(player, solid)) continue;
      if (axis === 'x') {
        if (player.vx > 0) {
          player.x = solid.x - player.width;
        } else if (player.vx < 0) {
          player.x = solid.x + solid.width;
        }
        player.vx = 0;
      } else {
        if (player.vy > 0) {
          player.y = solid.y - player.height;
          player.vy = 0;
          player.onGround = true;
        } else if (player.vy < 0) {
          player.y = solid.y + solid.height;
          player.vy = 0;
        }
      }
    }

    if (axis === 'x') {
      const maxX = world.width - player.width;
      if (player.x < 0) {
        player.x = 0;
        player.vx = 0;
      } else if (player.x > maxX) {
        player.x = maxX;
        player.vx = 0;
      }
    }
  }

  function movePlayer(delta) {
    player.x += player.vx * delta;
    resolveAxis('x');

    player.y += player.vy * delta;
    player.onGround = false;
    resolveAxis('y');
  }

  function checkMushrooms() {
    for (const mushroom of mushrooms) {
      if (mushroom.collected) continue;
      if (rectsIntersect(player, mushroom)) {
        mushroom.collected = true;
        player.mushrooms += 1;
        growPlayer();
        break;
      }
    }
  }

  function updateCamera() {
    const target = player.x + player.width / 2 - viewWidth / 2;
    camera.x = clamp(target, 0, world.width - viewWidth);
  }

  function respawn() {
    player.x = 120;
    player.y = groundY - player.height;
    player.vx = 0;
    player.vy = 0;
    player.onGround = false;
  }

  function updateClouds(delta) {
    for (const cloud of clouds) {
      cloud.x += cloud.speed * delta;
      if (cloud.x > world.width + 200) {
        cloud.x = -200;
      }
    }
  }

  function update(delta) {
    const accel = player.onGround ? player.groundAccel : player.airAccel;
    if (input.left) {
      player.vx -= accel * delta;
    }
    if (input.right) {
      player.vx += accel * delta;
    }

    if (!input.left && !input.right) {
      const drag = player.drag * delta;
      if (Math.abs(player.vx) <= drag) {
        player.vx = 0;
      } else {
        player.vx -= Math.sign(player.vx) * drag;
      }
    }

    player.vx = clamp(player.vx, -player.maxSpeed, player.maxSpeed);

    player.vy += physics.gravity * delta;
    player.vy = Math.min(player.vy, physics.maxFallSpeed);

    movePlayer(delta);
    checkMushrooms();
    updateCamera();
    updateClouds(delta);

    if (player.powerTimer > 0) {
      player.powerTimer = Math.max(0, player.powerTimer - delta);
    }

    if (player.y > viewHeight + 400) {
      respawn();
    }
  }

  function drawBackground() {
    const gradient = ctx.createLinearGradient(0, 0, 0, viewHeight);
    gradient.addColorStop(0, '#9de2ff');
    gradient.addColorStop(1, '#6dc0ff');
    ctx.fillStyle = gradient;
    ctx.fillRect(0, 0, viewWidth, viewHeight);

    ctx.save();
    ctx.translate(-camera.x * 0.3, 0);
    ctx.fillStyle = '#b8ecff';
    for (const cloud of clouds) {
      const width = 110 * cloud.scale;
      const height = 40 * cloud.scale;
      ctx.beginPath();
      ctx.ellipse(cloud.x, cloud.y, width, height, 0, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.restore();

    ctx.save();
    ctx.translate(-camera.x * 0.2, 0);
    ctx.fillStyle = '#62a05f';
    for (let i = 0; i < 5; i += 1) {
      const hillWidth = 320;
      const hillHeight = 120 + i * 10;
      const hillX = i * 620;
      ctx.beginPath();
      ctx.ellipse(hillX, groundY + hillHeight / 2, hillWidth, hillHeight, 0, 0, Math.PI * 2);
      ctx.fill();
    }
    ctx.restore();
  }

  function drawTerrain() {
    for (const solid of solids) {
      if (solid.x + solid.width < camera.x - 50) continue;
      if (solid.x > camera.x + viewWidth + 50) continue;
      ctx.fillStyle = solid.color;
      ctx.fillRect(solid.x, solid.y, solid.width, solid.height);
      if (solid.y >= groundY) {
        ctx.fillStyle = '#4ebc3c';
        ctx.fillRect(solid.x, solid.y, solid.width, 8);
      }
    }
  }

  function drawMushrooms() {
    const time = elapsed;
    for (const mushroom of mushrooms) {
      if (mushroom.collected) continue;
      const bob = Math.sin(time * 3 + mushroom.seed) * 4;
      const x = mushroom.x;
      const y = mushroom.y + bob;
      ctx.save();
      ctx.translate(x + mushroom.width / 2, y + mushroom.height);
      ctx.scale(mushroom.width / 32, mushroom.height / 32);
      ctx.translate(-16, -32);
      ctx.fillStyle = '#f04d3a';
      ctx.beginPath();
      ctx.ellipse(16, 12, 16, 12, 0, Math.PI, 0, true);
      ctx.fill();
      ctx.fillStyle = '#ffe8a3';
      ctx.fillRect(8, 12, 16, 12);
      ctx.fillStyle = '#fff9db';
      ctx.fillRect(12, 12, 8, 14);
      ctx.restore();
    }
  }

  function drawPlayer() {
    ctx.save();
    ctx.translate(player.x, player.y);

    const bodyColor = player.isBig ? '#d02323' : '#f04d3a';
    const hatColor = '#c60000';
    const skin = '#ffd7b5';

    const bodyHeight = player.height * 0.55;
    const legHeight = player.height - bodyHeight;

    // Legs
    ctx.fillStyle = '#5a3c2b';
    ctx.fillRect(6, bodyHeight, player.width - 12, legHeight);

    // Body
    ctx.fillStyle = bodyColor;
    ctx.fillRect(2, legHeight * 0.1, player.width - 4, bodyHeight * 0.9);

    // Head
    ctx.fillStyle = skin;
    ctx.beginPath();
    ctx.ellipse(player.width / 2, legHeight * 0.1, player.width * 0.45, player.height * 0.22, 0, 0, Math.PI * 2);
    ctx.fill();

    // Hat
    ctx.fillStyle = hatColor;
    ctx.beginPath();
    ctx.ellipse(player.width / 2, legHeight * 0.1 - player.height * 0.08, player.width * 0.55, player.height * 0.18, 0, Math.PI, 0, true);
    ctx.fill();

    ctx.restore();
  }

  function drawHUD() {
    ctx.save();
    ctx.fillStyle = 'rgba(0, 0, 0, 0.4)';
    ctx.fillRect(18, 18, 210, 94);
    ctx.fillStyle = '#fff';
    ctx.font = '600 20px "Segoe UI", sans-serif';
    ctx.fillText(`位置: ${Math.floor(player.x)} m`, 30, 44);
    ctx.fillText(`蘑菇: ${player.mushrooms}`, 30, 70);
    ctx.fillText(`身高: ${player.height}px`, 30, 96);

    if (player.powerTimer > 0) {
      ctx.font = '700 26px "Segoe UI", sans-serif';
      ctx.fillStyle = `rgba(255, 255, 255, ${0.5 + 0.5 * Math.sin(elapsed * 4)})`;
      ctx.textAlign = 'center';
      ctx.fillText('力量提升！', viewWidth / 2, 60);
      ctx.textAlign = 'left';
    }
    ctx.restore();
  }

  function draw() {
    ctx.save();
    ctx.clearRect(0, 0, viewWidth, viewHeight);
    ctx.restore();

    drawBackground();

    ctx.save();
    ctx.translate(-camera.x, 0);
    drawTerrain();
    drawMushrooms();
    drawPlayer();
    ctx.restore();

    drawHUD();
  }

  function loop(timestamp) {
    if (!lastTime) {
      lastTime = timestamp;
    }
    const delta = clamp((timestamp - lastTime) / 1000, 0, 0.05);
    lastTime = timestamp;
    elapsed += delta;
    update(delta);
    draw();
    requestAnimationFrame(loop);
  }

  function handleKeyDown(event) {
    const { code } = event;
    if (code === 'ArrowLeft' || code === 'KeyA') {
      input.left = true;
      event.preventDefault();
    }
    if (code === 'ArrowRight' || code === 'KeyD') {
      input.right = true;
      event.preventDefault();
    }
    if (code === 'ArrowUp' || code === 'KeyW' || code === 'Space') {
      attemptJump();
      event.preventDefault();
    }
  }

  function handleKeyUp(event) {
    const { code } = event;
    if (code === 'ArrowLeft' || code === 'KeyA') {
      input.left = false;
      event.preventDefault();
    }
    if (code === 'ArrowRight' || code === 'KeyD') {
      input.right = false;
      event.preventDefault();
    }
  }

  window.addEventListener('keydown', handleKeyDown);
  window.addEventListener('keyup', handleKeyUp);
  window.addEventListener('blur', () => {
    input.left = false;
    input.right = false;
  });

  requestAnimationFrame(loop);
})();

