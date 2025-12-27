#!/usr/bin/env python3
"""
Chinese-CLIP Embedding Service
使用 Chinese-CLIP 模型提供文本和图片的向量化服务
专门针对中文优化的跨模态检索模型
"""

import base64
import io
import logging
from typing import Dict, List

import torch
from flask import Flask, request, jsonify
from PIL import Image
from cn_clip.clip import load_from_name, tokenize

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

app = Flask(__name__)

# 全局模型变量
model = None
preprocess = None
device = None


def load_model():
    """加载 Chinese-CLIP 模型"""
    global model, preprocess, device

    logger.info("开始加载 Chinese-CLIP 模型（ViT-B-16）...")

    # 检测设备
    device = "cuda" if torch.cuda.is_available() else "cpu"
    logger.info(f"使用设备: {device}")

    # 加载模型 - 使用 Chinese-CLIP
    model_name = "ViT-B-16"
    logger.info(f"加载模型: Chinese-CLIP {model_name}（专门针对中文优化）")

    model, preprocess = load_from_name(model_name, device=device, download_root='./')
    model.eval()

    logger.info("✅ Chinese-CLIP 模型加载完成")
    # Chinese-CLIP 默认是 512 维（ViT-B-16）
    logger.info("向量维度: 512")


@app.route('/health', methods=['GET'])
def health():
    """健康检查"""
    return jsonify({
        "status": "healthy",
        "model": "Chinese-CLIP ViT-B-16",
        "dimension": 512,
        "device": str(device) if device else None
    })


@app.route('/embed/text', methods=['POST'])
def embed_text():
    """
    文本向量化

    Request Body:
    {
        "text": "要向量化的文本"
    }

    Response:
    {
        "embedding": [0.1, 0.2, ...],
        "dimension": 512
    }
    """
    try:
        data = request.get_json()
        text = data.get('text', '')

        if not text:
            return jsonify({"error": "文本不能为空"}), 400

        logger.info(f"处理文本: {text[:50]}...")

        # 文本编码 - 使用 Chinese-CLIP 的 tokenize
        text_input = tokenize([text]).to(device)

        # 提取特征
        with torch.no_grad():
            text_features = model.encode_text(text_input)
            # L2 归一化
            text_features = text_features / text_features.norm(dim=-1, keepdim=True)

        # 转换为列表
        embedding = text_features.cpu().numpy()[0].tolist()

        logger.info(f"✅ 文本向量化完成，维度: {len(embedding)}")

        return jsonify({
            "embedding": embedding,
            "dimension": len(embedding)
        })

    except Exception as e:
        logger.error(f"文本向量化失败: {str(e)}")
        return jsonify({"error": str(e)}), 500


@app.route('/embed/image', methods=['POST'])
def embed_image():
    """
    图片向量化

    Request Body:
    {
        "image": "base64编码的图片"
    }

    Response:
    {
        "embedding": [0.1, 0.2, ...],
        "dimension": 512
    }
    """
    try:
        data = request.get_json()
        image_b64 = data.get('image', '')

        if not image_b64:
            return jsonify({"error": "图片数据不能为空"}), 400

        logger.info("处理图片...")

        # Base64 解码
        image_data = base64.b64decode(image_b64)
        image = Image.open(io.BytesIO(image_data)).convert('RGB')

        logger.info(f"图片尺寸: {image.size}")

        # 图片编码 - 使用 Chinese-CLIP 的 preprocess
        image_input = preprocess(image).unsqueeze(0).to(device)

        # 提取特征
        with torch.no_grad():
            image_features = model.encode_image(image_input)
            # L2 归一化
            image_features = image_features / image_features.norm(dim=-1, keepdim=True)

        # 转换为列表
        embedding = image_features.cpu().numpy()[0].tolist()

        logger.info(f"✅ 图片向量化完成，维度: {len(embedding)}")

        return jsonify({
            "embedding": embedding,
            "dimension": len(embedding)
        })

    except Exception as e:
        logger.error(f"图片向量化失败: {str(e)}")
        return jsonify({"error": str(e)}), 500


@app.route('/embed/batch', methods=['POST'])
def embed_batch():
    """
    批量向量化（文本或图片）

    Request Body:
    {
        "texts": ["文本1", "文本2"],
        "images": ["base64_1", "base64_2"]
    }
    """
    try:
        data = request.get_json()
        texts = data.get('texts', [])
        images_b64 = data.get('images', [])

        results = {
            "text_embeddings": [],
            "image_embeddings": [],
            "dimension": 512
        }

        # 批量处理文本
        if texts:
            logger.info(f"批量处理 {len(texts)} 个文本")
            text_inputs = tokenize(texts).to(device)
            with torch.no_grad():
                text_features = model.encode_text(text_inputs)
                text_features = text_features / text_features.norm(dim=-1, keepdim=True)
            results["text_embeddings"] = text_features.cpu().numpy().tolist()

        # 批量处理图片
        if images_b64:
            logger.info(f"批量处理 {len(images_b64)} 张图片")
            images = []
            for img_b64 in images_b64:
                image_data = base64.b64decode(img_b64)
                img = Image.open(io.BytesIO(image_data)).convert('RGB')
                images.append(preprocess(img))

            # Stack all images
            image_inputs = torch.stack(images).to(device)

            with torch.no_grad():
                image_features = model.encode_image(image_inputs)
                image_features = image_features / image_features.norm(dim=-1, keepdim=True)
            results["image_embeddings"] = image_features.cpu().numpy().tolist()

        logger.info("✅ 批量向量化完成")
        return jsonify(results)

    except Exception as e:
        logger.error(f"批量向量化失败: {str(e)}")
        return jsonify({"error": str(e)}), 500


if __name__ == '__main__':
    # 启动时加载模型
    load_model()

    # 启动服务
    logger.info("启动 Chinese-CLIP Embedding 服务...")
    app.run(host='0.0.0.0', port=8888, debug=False)
