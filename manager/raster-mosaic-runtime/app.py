import os

from flask import Flask, jsonify, request, Response

from runtime.auth import require_internal_key
from runtime.overview_renderer import RasterMosaicRuntimeError, render_mosaic_tile


app = Flask(__name__)


@app.get("/health")
def health():
    return jsonify({"status": "ok", "service": "raster-mosaic-runtime"})


@app.post("/internal/raster-mosaic/render-tile")
def render_tile():
    auth_error = require_internal_key(request, os.environ.get("RASTER_MOSAIC_RUNTIME_INTERNAL_KEY", ""))
    if auth_error:
        return jsonify({"error": "unauthorized", "message": auth_error}), 401

    payload = request.get_json(silent=True) or {}
    try:
        rendered = render_mosaic_tile(payload)
    except RasterMosaicRuntimeError as exc:
        return jsonify({"error": exc.code, "message": str(exc)}), exc.status_code
    except Exception as exc:
        app.logger.exception("render raster mosaic tile failed")
        return jsonify({"error": "render_failed", "message": str(exc)}), 500

    response = Response(rendered.data, mimetype=rendered.content_type)
    if rendered.source:
        response.headers["X-ADDP-Mosaic-Tile-Source"] = rendered.source
    return response


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=int(os.environ.get("PORT", "8291")))
