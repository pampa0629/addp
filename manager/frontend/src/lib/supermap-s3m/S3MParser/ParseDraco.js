import S3MDracoDecode from './S3MDracoDecode.js'

function parseString(buffer, view, bytesOffset) {
    const length = view.getUint32(bytesOffset, true)
    bytesOffset += Uint32Array.BYTES_PER_ELEMENT
    const value = new TextDecoder('utf-8').decode(new Uint8Array(buffer, bytesOffset, length))
    return {
        string: value,
        bytesOffset: bytesOffset + length
    }
}

function alignToUint32(bytesOffset) {
    const remainder = bytesOffset % Uint32Array.BYTES_PER_ELEMENT
    return remainder === 0 ? bytesOffset : bytesOffset + Uint32Array.BYTES_PER_ELEMENT - remainder
}

export function readDracoAttributeInfo(view, bytesOffset, version) {
    const isVersion3 = Math.trunc(version) === 3
    if (isVersion3) {
        bytesOffset += Int32Array.BYTES_PER_ELEMENT
    }
    if (version >= 2) {
        bytesOffset += Int32Array.BYTES_PER_ELEMENT
    }

    const attributes = {
        posUniqueID: view.getInt32(bytesOffset, true)
    }
    bytesOffset += Int32Array.BYTES_PER_ELEMENT
    attributes.normalUniqueID = view.getInt32(bytesOffset, true)
    bytesOffset += Int32Array.BYTES_PER_ELEMENT
    attributes.colorUniqueID = view.getInt32(bytesOffset, true)
    bytesOffset += Int32Array.BYTES_PER_ELEMENT
    attributes.secondColorUniqueID = view.getInt32(bytesOffset, true)
    bytesOffset += Int32Array.BYTES_PER_ELEMENT

    const textureCoordinateCount = isVersion3
        ? view.getUint32(bytesOffset, true)
        : view.getUint16(bytesOffset, true)
    bytesOffset += isVersion3
        ? Uint32Array.BYTES_PER_ELEMENT
        : Uint16Array.BYTES_PER_ELEMENT

    attributes.texCoordUniqueIDs = []
    for (let index = 0; index < textureCoordinateCount; index += 1) {
        attributes.texCoordUniqueIDs.push(view.getInt32(bytesOffset, true))
        bytesOffset += Int32Array.BYTES_PER_ELEMENT
    }

    attributes.vertexAttrUniqueIDs = []
    if (version === 3.01) {
        const vertexAttributeID = view.getInt32(bytesOffset, true)
        bytesOffset += Int32Array.BYTES_PER_ELEMENT
        if (vertexAttributeID >= 0) {
            attributes.vertexAttrUniqueIDs.push(vertexAttributeID)
        }
    } else if (isVersion3) {
        const vertexAttributeCount = view.getUint32(bytesOffset, true)
        bytesOffset += Uint32Array.BYTES_PER_ELEMENT
        for (let index = 0; index < vertexAttributeCount; index += 1) {
            attributes.vertexAttrUniqueIDs.push(view.getInt32(bytesOffset, true))
            bytesOffset += Int32Array.BYTES_PER_ELEMENT
        }
    }

    return { attributes, bytesOffset }
}

export function parseDracoSkeleton(
    buffer,
    view,
    bytesOffset,
    vertexPackage,
    version,
    arrIndexPackage,
    dracoLib
) {
    const isVersion3 = Math.trunc(version) === 3
    const attributeInfo = readDracoAttributeInfo(view, bytesOffset, version)
    const attributes = attributeInfo.attributes
    bytesOffset = attributeInfo.bytesOffset

    const materialCount = view.getInt32(bytesOffset, true)
    bytesOffset += Int32Array.BYTES_PER_ELEMENT
    const indexPackage = {}
    if (materialCount > 0) {
        const material = parseString(buffer, view, bytesOffset)
        bytesOffset = material.bytesOffset
        indexPackage.materialCode = material.string
        arrIndexPackage.push(indexPackage)
    }

    if (isVersion3) {
        bytesOffset = alignToUint32(bytesOffset)
    }

    const compressedLength = view.getUint32(bytesOffset, true)
    bytesOffset += Uint32Array.BYTES_PER_ELEMENT
    if (compressedLength <= 0 || bytesOffset + compressedLength > buffer.byteLength) {
        throw new RangeError(`Invalid Draco payload length: ${compressedLength}`)
    }

    const compressedData = new Uint8Array(buffer, bytesOffset, compressedLength)
    if (materialCount > 0) {
        S3MDracoDecode.dracoDecodeMesh(
            dracoLib,
            compressedData,
            compressedLength,
            vertexPackage,
            indexPackage,
            attributes
        )
    } else {
        S3MDracoDecode.dracoDecodePointCloud(
            dracoLib,
            compressedData,
            compressedLength,
            vertexPackage,
            attributes
        )
    }
    bytesOffset += compressedLength

    if (isVersion3) {
        bytesOffset = alignToUint32(bytesOffset)
        const customAttributes = parseString(buffer, view, bytesOffset)
        bytesOffset = alignToUint32(customAttributes.bytesOffset)
        if (customAttributes.string) {
            vertexPackage.customVertexAttribute = JSON.parse(customAttributes.string)
        }
    }

    return bytesOffset
}
