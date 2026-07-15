import importlib.util
from pathlib import Path

BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


data_source_prompts = load_module("data_source_prompts_module", "pipelines/data_source_prompts.py")

build_query_analysis_prompt = data_source_prompts.build_query_analysis_prompt


def test_query_analysis_prompt_includes_contract_inputs():
    prompt = build_query_analysis_prompt("读取 pg public.roads", "FORMAT_JSON schema_name")

    assert "读取 pg public.roads" in prompt
    assert "FORMAT_JSON" in prompt
    assert "schema_name" in prompt
    assert "请直接返回 JSON" in prompt
