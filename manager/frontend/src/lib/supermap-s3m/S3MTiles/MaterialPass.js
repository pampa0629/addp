export function parseWebpMipPayload(imageBuffer) {
    if (!imageBuffer || imageBuffer.byteLength < Uint32Array.BYTES_PER_ELEMENT) {
        throw new Error('Invalid S3M WebP texture payload');
    }
    const view = new DataView(imageBuffer.buffer, imageBuffer.byteOffset, imageBuffer.byteLength);
    const levels = [];
    let offset = 0;
    while (offset < imageBuffer.byteLength) {
        if (imageBuffer.byteLength - offset < Uint32Array.BYTES_PER_ELEMENT) {
            throw new Error('Invalid S3M WebP mip length');
        }
        const length = view.getUint32(offset, true);
        offset += Uint32Array.BYTES_PER_ELEMENT;
        if (length <= 0 || offset + length > imageBuffer.byteLength) {
            throw new Error('Invalid S3M WebP mip payload');
        }
        levels.push(imageBuffer.subarray(offset, offset + length));
        offset += length;
    }
    return levels;
}

export function isCompleteWebpMipChain(levels) {
    if (!Array.isArray(levels) || levels.length === 0) return false;
    let width = Number(levels[0]?.width);
    let height = Number(levels[0]?.height);
    if (!Number.isInteger(width) || width <= 0 || !Number.isInteger(height) || height <= 0) return false;
    for (let i = 1; i < levels.length; i++) {
        width = Math.max(width >> 1, 1);
        height = Math.max(height >> 1, 1);
        if (Number(levels[i]?.width) !== width || Number(levels[i]?.height) !== height) return false;
    }
    return width === 1 && height === 1;
}

function decodedImageSize(image) {
    return {
        width: Number(image?.naturalWidth || image?.videoWidth || image?.width || 0),
        height: Number(image?.naturalHeight || image?.videoHeight || image?.height || 0)
    };
}

function uploadWebpMipImages(context, texture, decodedImages) {
    const gl = context._gl;
    const activeTexture = gl.getParameter(gl.ACTIVE_TEXTURE);
    gl.activeTexture(gl.TEXTURE0);
    const boundTexture = gl.getParameter(gl.TEXTURE_BINDING_2D);
    const unpackAlignment = gl.getParameter(gl.UNPACK_ALIGNMENT);
    const unpackFlipY = gl.getParameter(gl.UNPACK_FLIP_Y_WEBGL);
    const unpackPremultiplyAlpha = gl.getParameter(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL);
    const unpackColorSpace = gl.getParameter(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL);

    try {
        gl.bindTexture(gl.TEXTURE_2D, texture._texture);
        gl.pixelStorei(gl.UNPACK_ALIGNMENT, 4);
        gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
        gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false);
        gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.BROWSER_DEFAULT_WEBGL);
        for (let level = 1; level < decodedImages.length; level++) {
            gl.texImage2D(
                gl.TEXTURE_2D,
                level,
                gl.RGBA,
                gl.RGBA,
                gl.UNSIGNED_BYTE,
                decodedImages[level]
            );
        }
    } finally {
        gl.pixelStorei(gl.UNPACK_ALIGNMENT, unpackAlignment);
        gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, unpackFlipY);
        gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, unpackPremultiplyAlpha);
        gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, unpackColorSpace);
        gl.bindTexture(gl.TEXTURE_2D, boundTexture);
        gl.activeTexture(activeTexture);
    }
}

function MaterialPass(){
    this.ambientColor = new Cesium.Color();
    this.diffuseColor = new Cesium.Color();
    this.specularColor = new Cesium.Color(0.0, 0.0, 0.0, 0.0);
    this.shininess = 50.0;
    this.bTransparentSorting = false;
    this.alphaMode = undefined;
    this.texMatrix = Cesium.Matrix4.clone(Cesium.Matrix4.IDENTITY, new Cesium.Matrix4());
    this.textures = [];
    this._RGBTOBGR = false;
}

MaterialPass.prototype.isDestroyed = function() {
    return false;
};

MaterialPass.prototype.destroy = function(){
    let length = this.textures.length;
    for(let i = 0;i < length;i++){
        let texture = this.textures[i];
        texture.destroy();
    }

    this.textures.length = 0;
    this.ambientColor = undefined;
    this.diffuseColor = undefined;
    this.specularColor = undefined;
    return Cesium.destroyObject(this);
};

MaterialPass.prototype.createCRN = function(textureCode,textureName,context, index, imgObj, wrapS, wrapT, isStandard, mipmapEnabled, textureTypeObj) {
    var promise;
    mipmapEnabled = defaultValue(mipmapEnabled, true);
    if (isStandard) {
        promise = loadCRN(imgObj.imageBuffer, true, true);
    } else {
        if (S3MTaskManager.CRNTaskProcessorReady) {
            promise = loadCRNForS3M(S3MTaskManager.CRNProcessor, imgObj.imageBuffer, true);
        }
    }
    if(!defined(promise)){
        return;
    }
    var that = this;
    promise.then(function(img) {
        if(that.isDestroyed()){
            return ;
        }
        textureTypeObj = defaultValue(textureTypeObj, {});
        var texture = DDSTextureManager.CreateTexture(textureCode, context,imgObj.width, imgObj.height,imgObj.nFormat, S3MCompressType.enrS3TCDXTN, img.bufferView,false, wrapS, wrapT, mipmapEnabled);
        if(textureTypeObj.isEmissiveTex){
            that.emissiveTexture = texture;
        }
        else if(textureTypeObj.isNormalTexture){
            that.normalTexture = texture;
        }
        else{
            if(index === 0 && that._textures.length > 0){
                that._textures.splice(0,0,texture);
            }
            else{
                that._textures.push(texture);
            }
        }

    });
    return promise;
};


MaterialPass.prototype.createWebp = async function(keyword, context, textureInfo) {
    let mipmapEnabled = Cesium.defaultValue(textureInfo.mipmapEnabled, true);
    let imageBuffer = textureInfo.arrayBufferView;
    let wrapS = textureInfo.wrapS;
    let wrapT = textureInfo.wrapT;
    const webpLevels = parseWebpMipPayload(imageBuffer);
    const decodedImages = await Promise.all(webpLevels.map(webpBuffer => Cesium.loadImageFromTypedArray({
        uint8Array: webpBuffer,
        format: 'image/webp'
    })));

    var that = this;
    if (decodedImages.some(image => !Cesium.defined(image)) || that.isDestroyed()) {
        decodedImages.forEach(image => image?.close?.());
        return;
    }
    const baseLevel = decodedImageSize(decodedImages[0]);
    if (!baseLevel.width || !baseLevel.height) {
        decodedImages.forEach(image => image?.close?.());
        throw new Error('Invalid decoded S3M WebP image');
    }
    const useMipmaps = mipmapEnabled &&
        Cesium.Math.isPowerOfTwo(baseLevel.width) &&
        Cesium.Math.isPowerOfTwo(baseLevel.height) &&
        isCompleteWebpMipChain(decodedImages);
    if (!Cesium.Math.isPowerOfTwo(baseLevel.width) || !Cesium.Math.isPowerOfTwo(baseLevel.height)) {
        wrapS = Cesium.TextureWrap.CLAMP_TO_EDGE;
        wrapT = Cesium.TextureWrap.CLAMP_TO_EDGE;
    }
    let texture;
    try {
        texture = new Cesium.Texture({
            context : context,
            source : decodedImages[0],
            pixelFormat : Cesium.PixelFormat.RGBA,
            flipY : false,
            sampler : new Cesium.Sampler({
                wrapS : wrapS,
                wrapT : wrapT,
                minificationFilter : Cesium.TextureMinificationFilter.LINEAR,
                magnificationFilter : Cesium.TextureMagnificationFilter.LINEAR
            })
        });
        if (useMipmaps) {
            uploadWebpMipImages(context, texture, decodedImages);
            texture.sampler = new Cesium.Sampler({
                wrapS : wrapS,
                wrapT : wrapT,
                minificationFilter : Cesium.TextureMinificationFilter.LINEAR_MIPMAP_LINEAR,
                magnificationFilter : Cesium.TextureMagnificationFilter.LINEAR
            });
        }
        context.textureCache.addTexture(keyword, texture);
        that.textures.push(texture);
    } catch (error) {
        if (texture && !texture.isDestroyed()) texture.destroy();
        throw error;
    } finally {
        decodedImages.forEach(image => image?.close?.());
    }
};

export default MaterialPass;
