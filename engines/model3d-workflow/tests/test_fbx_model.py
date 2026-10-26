import base64
import json
import os
from pathlib import Path

import numpy as np
import pytest

from fbx_model import MAX_BYTES, PROFILE, parse_fbx, stage_fbx_source
from glb_validation import validate_glb
from operators import CommandResult, ConverterError, invoke_operator
from .glb_fixture import triangle_doc, glb_bytes
from .test_operators import directory_plan, file_plan

ROOT = Path(__file__).resolve().parents[3]
CORPUS = json.loads((ROOT/'common/format/plugins/fbx/testdata/composition-cases.json').read_text())


@pytest.mark.parametrize('case', CORPUS['cases'], ids=lambda c:c['name'])
def test_shared_fbx_composition_profile(case):
    raw = base64.b64decode(case['base64'])
    if not case['valid']:
        with pytest.raises(ValueError):parse_fbx(raw)
    else:
        assert parse_fbx(raw) == {'profile':PROFILE,'version':7400,'encoding':'binary','mesh_count':case['mesh_count']}


@pytest.mark.parametrize('case', [c for c in CORPUS['cases'] if not c['valid']], ids=lambda c:c['name'])
def test_out_of_profile_fbx_does_not_call_converter_or_replace(tmp_path, case):
    source=tmp_path/'source.fbx';source.write_bytes(base64.b64decode(case['base64']))
    target=tmp_path/'ready.glb';target.write_bytes(b'previous ready artifact')
    plan=file_plan(source,target,'fbx','glb');plan['target']['write_mode']='replace'
    def forbidden(*args):raise AssertionError('converter invoked for unsupported FBX')
    with pytest.raises(ConverterError) as failure:
        invoke_operator('fbx_to_glb',{'access_plan':plan},runner=forbidden)
    assert failure.value.error_code=='UNSUPPORTED_MODEL_SOURCE'
    assert target.read_bytes()==b'previous ready artifact'


def test_fbx_uses_same_verified_private_bytes_and_removes_snapshot(tmp_path):
    raw=base64.b64decode(CORPUS['cases'][1]['base64']);source=tmp_path/'source.fbx';source.write_bytes(raw)
    target=tmp_path/'result.glb';staged=[]
    def runner(command, timeout_seconds):
        copy=Path(command[2]);staged.append(copy)
        assert copy!=source and copy.read_bytes()==raw
        source.write_bytes(b'changed after source read')
        assert copy.read_bytes()==raw
        Path(command[3]).write_bytes(glb_bytes(*triangle_doc()))
        return CommandResult(0)
    result=invoke_operator('fbx_to_glb',{'access_plan':file_plan(source,target,'fbx','glb')},runner=runner,
                           env={'MODEL3D_MESH_CONVERTER_BIN':'/bound/native/assimp'})
    assert result['source_profile']==PROFILE and target.exists()
    assert staged and not staged[0].parent.exists()


@pytest.mark.parametrize('kind',['directory','wrong-format'])
def test_fbx_rejects_non_file_fbx_access_plan(tmp_path, kind):
    source=tmp_path/'model.fbx';source.write_bytes(base64.b64decode(CORPUS['cases'][0]['base64']))
    target=tmp_path/'output.glb'
    plan=directory_plan(tmp_path,target,'fbx','glb',entrypoint=source.name) if kind=='directory' else file_plan(source,target,'obj','glb')
    with pytest.raises(ConverterError) as error:invoke_operator('fbx_to_glb',{'access_plan':plan})
    assert error.value.error_code=='UNSUPPORTED_MODEL_SOURCE' and not target.exists()


def test_fbx_source_rejects_symlink_fifo_and_budget_before_copy(tmp_path):
    source=tmp_path/'real.fbx';source.write_bytes(base64.b64decode(CORPUS['cases'][0]['base64']))
    link=tmp_path/'link.fbx';link.symlink_to(source)
    with pytest.raises(OSError):stage_fbx_source(link,tmp_path)
    fifo=tmp_path/'pipe.fbx';os.mkfifo(fifo)
    with pytest.raises(ValueError):stage_fbx_source(fifo,tmp_path)
    large=tmp_path/'large.fbx'
    with large.open('wb') as f:f.truncate(MAX_BYTES+1)
    with pytest.raises(ValueError):stage_fbx_source(large,tmp_path)
    assert not (tmp_path/'validated-source.fbx').exists()


@pytest.mark.skipif(not os.environ.get('MODEL3D_MESH_CONVERTER_BIN'), reason='real native converter must be explicitly bound')
def test_native_fbx_two_metre_triangles_opaque_material_and_original_bytes(tmp_path, record_property):
    import trimesh
    source=ROOT/'engines/model3d-workflow/tests/fixtures/mesh/static-two-triangles.fbx'
    before=source.read_bytes();target=tmp_path/'model.glb'
    result=invoke_operator('fbx_to_glb',{'access_plan':file_plan(source,target,'fbx','glb')},timeout_seconds=60)
    assert source.read_bytes()==before and result['source_profile']==PROFILE
    doc=validate_glb(target,basic_static=True)
    assert not doc.get('images') and not doc.get('animations') and not doc.get('skins')
    scene=trimesh.load(target,force='scene')
    np.testing.assert_allclose(scene.bounds,[[0,0,0],[6,3,1]],atol=1e-6)
    triangles=[sorted(tuple(round(float(v),6) for v in vertex) for vertex in triangle)
               for mesh in scene.dump() for triangle in mesh.triangles]
    expected=[[(0.,0.,0.),(0.,3.,1.),(2.,0.,0.)],[(4.,0.,0.),(4.,3.,1.),(6.,0.,0.)]]
    assert sorted(triangles)==expected and len(scene.graph.nodes_geometry)==2
    for mesh in doc['meshes']:
        for primitive in mesh['primitives']:
            material=doc['materials'][primitive['material']]
            np.testing.assert_allclose(material['pbrMetallicRoughness']['baseColorFactor'],[.2,.4,.8,1],atol=1e-6)
            assert material.get('alphaMode','OPAQUE')=='OPAQUE'
    record_property('fbx_profile',PROFILE);record_property('native_triangles',2)
