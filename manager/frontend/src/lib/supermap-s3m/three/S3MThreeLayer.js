import * as THREE from 'three'
import S3ModelParser from '../S3MParser/S3ModelParser.js'
import {
  createS3MThreeContent,
  disposeS3MThreeResources,
  resolveS3MResourceURL,
  s3mRootTilesBoundingBox,
  s3mRootTilesBoundingSphere
} from './S3MThreeContent.js'

const TILE_UNLOADED = 'unloaded'
const TILE_QUEUED = 'queued'
const TILE_LOADING = 'loading'
const TILE_READY = 'ready'
const TILE_FAILED = 'failed'

const RANGE_DISTANCE = 0
const RANGE_PIXEL = 1
const RANGE_GEOMETRY_ERROR = 2

function createTile(url, options = {}) {
  return {
    url,
    isRoot: Boolean(options.isRoot),
    depth: Number(options.depth || 0),
    state: TILE_UNLOADED,
    group: null,
    pageLods: [],
    resources: null,
    abortController: null,
    promise: null,
    resolve: null,
    reject: null,
    lastUsedFrame: 0,
    stats: null
  }
}

function tileVisible(tile) {
  return tile.pageLods.some(page => page.group.visible)
}

export default class S3MThreeLayer {
  constructor(options) {
    this.url = options.url
    this.camera = options.camera
    this.renderer = options.renderer
    this.headers = options.headers || {}
    this.onProgress = options.onProgress || (() => {})
    this.onTileError = options.onTileError || (() => {})
    this.group = new THREE.Group()
    this.group.name = 'S3MThreeLayer'
    this.rootTiles = []
    this.loadedTiles = new Set()
    this.requestQueue = []
    this.activeRequests = 0
    this.maxRequests = Number(options.maxRequests || 6)
    this.maxCachedTiles = Number(options.maxCachedTiles || 256)
    this.frame = 0
    this.disposed = false
    this.manifestAbortController = null
    this.frustum = new THREE.Frustum()
    this.projectionViewMatrix = new THREE.Matrix4()
    this.stats = {
      loadedTiles: 0,
      meshes: 0,
      vertices: 0,
      triangles: 0
    }
  }

  async load() {
    if (this.disposed) throw new Error('S3M Three layer is disposed')
    await S3ModelParser.readyPromise
    this.manifestAbortController = new AbortController()
    const response = await fetch(this.url, {
      headers: this.headers,
      cache: 'no-store',
      signal: this.manifestAbortController.signal
    })
    this.manifestAbortController = null
    if (!response.ok) throw new Error(`S3M manifest HTTP ${response.status}`)
    const manifest = await response.json()
    const rootEntries = Array.isArray(manifest.rootTiles) ? manifest.rootTiles : []
    if (!rootEntries.length) throw new Error('S3M manifest has no root tiles')
    this.rootTiles = rootEntries.map(entry => createTile(
      resolveS3MResourceURL(entry.url, this.url),
      { isRoot: true, depth: 0 }
    ))
    await Promise.all(this.rootTiles.map(tile => this.requestTile(tile, 0)))
    this.update()
    return manifest
  }

  requestTile(tile, priority = 0) {
    if (this.disposed) return Promise.reject(new Error('S3M Three layer is disposed'))
    if (tile.state === TILE_READY) return Promise.resolve(tile)
    if (tile.promise) return tile.promise
    tile.state = TILE_QUEUED
    tile.promise = new Promise((resolve, reject) => {
      tile.resolve = resolve
      tile.reject = reject
    })
    this.requestQueue.push({ tile, priority })
    this.requestQueue.sort((left, right) => left.priority - right.priority)
    this.pumpRequests()
    return tile.promise
  }

  pumpRequests() {
    while (!this.disposed && this.activeRequests < this.maxRequests && this.requestQueue.length) {
      const { tile } = this.requestQueue.shift()
      if (tile.state !== TILE_QUEUED) continue
      this.activeRequests += 1
      this.loadTile(tile).finally(() => {
        this.activeRequests -= 1
        this.pumpRequests()
      })
    }
  }

  async loadTile(tile) {
    tile.state = TILE_LOADING
    tile.abortController = new AbortController()
    try {
      const response = await fetch(tile.url, {
        headers: this.headers,
        cache: 'no-store',
        signal: tile.abortController.signal
      })
      if (!response.ok) throw new Error(`S3M tile HTTP ${response.status}: ${tile.url}`)
      const content = await S3ModelParser.parseBuffer(await response.arrayBuffer())
      if (!content) throw new Error(`Invalid S3M tile: ${tile.url}`)
      if (this.disposed) throw new DOMException('S3M Three layer disposed', 'AbortError')
      const built = createS3MThreeContent(content, tile.url)
      if (this.disposed) {
        disposeS3MThreeResources(built.resources)
        throw new DOMException('S3M Three layer disposed', 'AbortError')
      }
      tile.resources = built.resources
      tile.pageLods = built.pageLods
      tile.stats = built.stats
      tile.group = new THREE.Group()
      tile.group.name = tile.url
      for (const page of tile.pageLods) tile.group.add(page.group)
      this.group.add(tile.group)
      tile.state = TILE_READY
      tile.lastUsedFrame = this.frame
      this.loadedTiles.add(tile)
      this.stats.loadedTiles = this.loadedTiles.size
      this.stats.meshes += built.stats.meshes
      this.stats.vertices += built.stats.vertices
      this.stats.triangles += built.stats.triangles
      this.onProgress({ ...this.stats, pendingRequests: this.requestQueue.length + this.activeRequests })
      tile.resolve?.(tile)
    } catch (error) {
      if (error?.name === 'AbortError' || this.disposed) {
        tile.reject?.(error)
        return
      }
      tile.state = TILE_FAILED
      tile.reject?.(error)
    } finally {
      tile.abortController = null
      tile.resolve = null
      tile.reject = null
      tile.promise = null
    }
  }

  shouldRefine(page) {
    if (!page.childURL || !page.sphere || !this.camera || !this.renderer) return false
    const distance = Math.max(
      this.camera.position.distanceTo(page.sphere.center) - page.sphere.radius,
      0.01
    )
    if (page.rangeMode === RANGE_DISTANCE) return distance < page.rangeList
    const height = Math.max(1, this.renderer.domElement.clientHeight)
    const pixelsPerUnit = height / (2 * Math.tan(THREE.MathUtils.degToRad(this.camera.fov) / 2))
    if (page.rangeMode === RANGE_PIXEL) {
      return page.sphere.radius * pixelsPerUnit / distance > page.rangeList
    }
    if (page.rangeMode === RANGE_GEOMETRY_ERROR) {
      return page.rangeList * pixelsPerUnit / distance > 16
    }
    return false
  }

  pageVisible(page) {
    return !page.sphere || this.frustum.intersectsSphere(page.sphere)
  }

  selectTile(tile) {
    if (tile.state !== TILE_READY) return false
    tile.lastUsedFrame = this.frame
    let selected = false
    for (const page of tile.pageLods) {
      if (!this.pageVisible(page)) continue
      const refine = tile.depth < 32 && this.shouldRefine(page)
      if (!refine) {
        page.group.visible = true
        selected = true
        continue
      }
      if (!page.child) {
        page.child = createTile(page.childURL, { depth: tile.depth + 1 })
      }
      const child = page.child
      if (child.state === TILE_READY && this.selectTile(child)) {
        page.group.visible = false
        selected = true
        continue
      }
      page.group.visible = true
      selected = true
      if (child.state === TILE_UNLOADED) {
        this.requestTile(child, tile.depth + 1).catch((error) => {
          if (error?.name !== 'AbortError' && !this.disposed) this.onTileError(error)
        })
      }
    }
    return selected
  }

  update() {
    if (this.disposed) return
    this.frame += 1
    this.camera.updateMatrixWorld()
    this.projectionViewMatrix.multiplyMatrices(this.camera.projectionMatrix, this.camera.matrixWorldInverse)
    this.frustum.setFromProjectionMatrix(this.projectionViewMatrix)
    for (const tile of this.loadedTiles) {
      for (const page of tile.pageLods) page.group.visible = false
    }
    for (const root of this.rootTiles) this.selectTile(root)
    this.evictTiles()
  }

  evictTiles() {
    const overflow = this.loadedTiles.size - this.maxCachedTiles
    if (overflow <= 0) return
    const candidates = [...this.loadedTiles]
      .filter(tile => !tile.isRoot && !tileVisible(tile) && this.frame - tile.lastUsedFrame > 120)
      .sort((left, right) => left.lastUsedFrame - right.lastUsedFrame)
      .slice(0, overflow)
    for (const tile of candidates) this.disposeTileContent(tile)
  }

  getBoundingSphere() {
    return s3mRootTilesBoundingSphere(this.rootTiles)
  }

  getBoundingBox() {
    return s3mRootTilesBoundingBox(this.rootTiles)
  }

  disposeTileContent(tile) {
    tile.abortController?.abort()
    tile.reject?.(new DOMException('S3M tile request cancelled', 'AbortError'))
    for (const page of tile.pageLods) {
      if (page.child) this.disposeTileContent(page.child)
    }
    if (tile.group) this.group.remove(tile.group)
    disposeS3MThreeResources(tile.resources)
    tile.group = null
    tile.pageLods = []
    tile.resources = null
    if (tile.stats) {
      this.stats.meshes = Math.max(0, this.stats.meshes - tile.stats.meshes)
      this.stats.vertices = Math.max(0, this.stats.vertices - tile.stats.vertices)
      this.stats.triangles = Math.max(0, this.stats.triangles - tile.stats.triangles)
    }
    tile.stats = null
    tile.state = TILE_UNLOADED
    tile.promise = null
    this.loadedTiles.delete(tile)
    this.stats.loadedTiles = this.loadedTiles.size
  }

  dispose() {
    if (this.disposed) return
    this.disposed = true
    this.manifestAbortController?.abort()
    this.manifestAbortController = null
    for (const { tile } of this.requestQueue) {
      tile.reject?.(new Error('S3M Three layer disposed'))
      tile.promise = null
      tile.state = TILE_UNLOADED
    }
    this.requestQueue = []
    for (const root of this.rootTiles) this.disposeTileContent(root)
    this.rootTiles = []
    this.loadedTiles.clear()
    this.group.removeFromParent()
  }
}
