#!/usr/bin/env python3
"""
测试阿里云 DashScope 多模态向量化模型
对比 tongyi-embedding-vision-flash, tongyi-embedding-vision-plus, qwen2.5-vl-embedding
"""

import dashscope
import os
import time
import base64

# 设置 API Key
API_KEY = "sk-d99b9419a7ee444ea54f91700ba30ade"

# 测试的模型列表
MODELS = [
    "tongyi-embedding-vision-flash",
    "tongyi-embedding-vision-plus",
    "qwen2.5-vl-embedding"
]

# 测试图片路径
IMAGE_PATH = "/Users/pampa/code/addp/labs/data/开会.jpg"


def read_image_base64(image_path):
    """读取图片并转为 base64"""
    with open(image_path, "rb") as f:
        image_data = f.read()
    return base64.b64encode(image_data).decode('utf-8')


def test_text_embedding(model_name, text):
    """测试文本向量化"""
    print(f"\n{'='*60}")
    print(f"模型: {model_name}")
    print(f"测试: 文本向量化")
    print(f"输入: {text}")
    print(f"{'='*60}")

    input_data = [{'text': text}]

    start_time = time.time()
    try:
        resp = dashscope.MultiModalEmbedding.call(
            api_key=API_KEY,
            model=model_name,
            input=input_data
        )
        latency = (time.time() - start_time) * 1000  # ms

        if resp.status_code == 200:
            embedding = resp.output['embeddings'][0]['embedding']
            dimension = len(embedding)
            print(f"✅ 成功")
            print(f"   维度: {dimension}")
            print(f"   延迟: {latency:.1f} ms")
            print(f"   前5维: {embedding[:5]}")
            return embedding
        else:
            print(f"❌ 失败: {resp.code} - {resp.message}")
            return None

    except Exception as e:
        print(f"❌ 异常: {str(e)}")
        return None


def test_image_embedding(model_name, image_path):
    """测试图片向量化"""
    print(f"\n{'='*60}")
    print(f"模型: {model_name}")
    print(f"测试: 图片向量化")
    print(f"输入: {image_path}")
    print(f"{'='*60}")

    # 读取图片
    image_b64 = read_image_base64(image_path)
    input_data = [{'image': f'data:image/jpeg;base64,{image_b64}'}]

    start_time = time.time()
    try:
        resp = dashscope.MultiModalEmbedding.call(
            api_key=API_KEY,
            model=model_name,
            input=input_data
        )
        latency = (time.time() - start_time) * 1000  # ms

        if resp.status_code == 200:
            embedding = resp.output['embeddings'][0]['embedding']
            dimension = len(embedding)
            print(f"✅ 成功")
            print(f"   维度: {dimension}")
            print(f"   延迟: {latency:.1f} ms")
            print(f"   前5维: {embedding[:5]}")
            return embedding
        else:
            print(f"❌ 失败: {resp.code} - {resp.message}")
            return None

    except Exception as e:
        print(f"❌ 异常: {str(e)}")
        return None


def cosine_similarity(vec1, vec2):
    """计算余弦相似度"""
    import math

    # 计算点积
    dot_product = sum(a * b for a, b in zip(vec1, vec2))

    # 计算模长
    norm1 = math.sqrt(sum(a * a for a in vec1))
    norm2 = math.sqrt(sum(b * b for b in vec2))

    # 余弦相似度
    return dot_product / (norm1 * norm2) if norm1 * norm2 != 0 else 0


def main():
    print("\n" + "="*80)
    print("阿里云 DashScope 多模态向量化模型测试")
    print("="*80)

    # 测试查询
    queries = [
        "开会",
        "meeting",
        "天空",
        "汽车"
    ]

    results = {}

    # 对每个模型进行测试
    for model_name in MODELS:
        print(f"\n\n{'#'*80}")
        print(f"# 测试模型: {model_name}")
        print(f"{'#'*80}")

        # 1. 向量化图片
        image_embedding = test_image_embedding(model_name, IMAGE_PATH)
        if image_embedding is None:
            print(f"\n⚠️ 模型 {model_name} 图片向量化失败，跳过")
            continue

        # 2. 向量化查询文本并计算相似度
        model_results = []
        for query in queries:
            text_embedding = test_text_embedding(model_name, query)
            if text_embedding:
                similarity = cosine_similarity(text_embedding, image_embedding)
                model_results.append({
                    'query': query,
                    'similarity': similarity
                })
                print(f"   与图片相似度: {similarity:.4f}")

        results[model_name] = model_results

    # 打印对比结果
    print("\n\n" + "="*80)
    print("综合对比结果")
    print("="*80)

    if not results:
        print("⚠️ 没有成功的测试结果")
        return

    print(f"\n{'查询词':<15}", end="")
    for model_name in results.keys():
        print(f"{model_name:<35}", end="")
    print()
    print("-" * 80)

    for query in queries:
        print(f"{query:<15}", end="")
        for model_name in results.keys():
            model_results = results[model_name]
            similarity = next((r['similarity'] for r in model_results if r['query'] == query), 0)
            print(f"{similarity:<35.4f}", end="")
        print()

    print("\n" + "="*80)


if __name__ == "__main__":
    main()
