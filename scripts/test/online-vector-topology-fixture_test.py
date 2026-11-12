import copy
import importlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

m = importlib.import_module('business.scripts.online-vector-topology-fixture')


def records(name):
    if name in ('line-chains.gpkg','line-chain-members.gpkg'):
        return m.line_chain_expected_features(members=name=='line-chain-members.gpkg')
    if name == 'line-components.gpkg':
        return m.line_component_expected_features()
    if name == 'split-lines.gpkg':
        return m.split_line_expected_features()
    if name == 'line-self-intersections.gpkg':
        return m.line_self_intersection_expected_features()
    if name == 'line-intersections.gpkg':
        return m.line_intersection_expected_features()
    if name == 'line-self-overlaps.gpkg':
        return m.line_self_overlap_expected_features()
    if name == 'line-overlaps.gpkg':
        return m.line_overlap_expected_features()
    if name == 'dangling.gpkg':
        return m.dangling_expected_features()
    if name == 'gaps.gpkg':
        return m.gap_expected_features()
    if name == 'overlaps.gpkg':
        return m.overlap_expected_features()
    if name == 'snapped.gpkg':
        return [{'geometry': {'type': 'LineString', 'coordinates': [list(point) for point in points]},
                 'properties': {'source_name': f'snap-{index}', 'grade': 10+index}}
                for index, points in enumerate(m.SNAP_EXPECTED)]
    if name == 'repaired.geojson':
        return [{'geometry': {'type': 'MultiPolygon', 'coordinates': [
            [[[x,30],[x+2,30],[x+1,31],[x,30]]], [[[x,32],[x+2,32],[x+1,31],[x,32]]]]},
            'properties': {'source_name': f'bow-{i}', 'grade': 7+i, 'geometry_valid': False,
                'geometry_reason': 'Self-intersection', 'after_valid': True, 'after_reason': 'Valid Geometry'}}
            for i,x in enumerate((120,124))]
    if name == 'faces.geojson':
        coordinates = [[[[x,y],[x+2,y],[x+2,y+2],[x,y+2],[x,y]]] for x in (120,122) for y in (30,32)]
        return [{'geometry': {'type': 'Polygon', 'coordinates': c}, 'properties': {}} for c in coordinates]
    return [{'geometry': {'type': 'LineString', 'coordinates': c}, 'properties': {'issue_type': 'dangle'}} for c in (
        [[119,32],[120,32]], [[124,32],[125,32]], [[122,29],[122,30]], [[122,34],[122,35]])]


class TopologyFixtureTest(unittest.TestCase):
    def test_endpoint_handwritten_oracle_types_all_references_and_exact_endpoints(self):
        self.assertEqual([len(m.endpoint_features(k)) for k in m.ENDPOINT_KINDS],[12,14,4,2,1,1,1])
        self.assertEqual(len(m.endpoint_features('details')),10)
        self.assertEqual(m.endpoint_features('details')[0]['geometry'],{'type':'Point','coordinates':[0,0]})
        self.assertEqual([r['properties']['reference_id'] for r in m.endpoint_features('details')][:2],['z','a'])
        for kind in (*m.ENDPOINT_KINDS,'result','details'):
            original=m.endpoint_features(kind);m.validate_extension_features(original,kind,m.ENDPOINT_CASE)
            for mutate in (lambda r:r.pop(),lambda r:r.reverse(),lambda r:r[0]['properties'].update({next(iter(r[0]['properties'])):'wrong'})):
                import copy
                rows=copy.deepcopy(original);mutate(rows)
                if rows!=original:
                    with self.subTest(kind=kind),self.assertRaises(m.FixtureError):m.validate_extension_features(rows,kind,m.ENDPOINT_CASE)

    def test_extension_handwritten_oracle_preserves_types_all_sources_direction_and_exact_boundaries(self):
        self.assertEqual([len(m.extension_features(k)) for k in ('source','reference','failure_reference','result','details')],[7,14,1,7,8])
        self.assertEqual([r['properties']['reference_id'] for r in m.extension_features('details')],['start','first','duplicate','collinear','forward','part-one','part-two','micro'])
        for kind in ('source','reference','failure_reference','result','details'):
            original=m.extension_features(kind);m.validate_extension_features(original,kind)
            for mutate in [lambda r:r.pop(),lambda r:r.append(copy.deepcopy(r[0])),lambda r:r[0]['properties'].update(grade=3.0) if 'grade' in r[0]['properties'] else r[0]['properties'].update(note='foreign'),
                lambda r:r[0]['geometry']['coordinates'].reverse()]:
                rows=copy.deepcopy(original);mutate(rows)
                if rows!=original:
                    with self.subTest(kind=kind,mutate=mutate),self.assertRaises(m.FixtureError):m.validate_extension_features(rows,kind)
            if kind in ('source','result'):
                rows=copy.deepcopy(original);rows[0]['properties']['enabled']=1
                with self.assertRaises(m.FixtureError):m.validate_extension_features(rows,kind)
        details=m.extension_features('details');details[-1]['properties']['distance']=0.
        with self.assertRaises(m.FixtureError):m.validate_extension_features(details,'details')

    def test_chain_oracles_preserve_all_members_and_reject_wrong_chain_direction_schema_and_microgap(self):
        self.assertEqual(len(m.line_chain_source_features()),16)
        self.assertEqual(len(records('line-chains.gpkg')),12)
        self.assertEqual(len(records('line-chain-members.gpkg')),18)
        for name in ['line-chains.gpkg','line-chain-members.gpkg']:
            original=records(name);m.validate_features(name,original)
            mutations=[lambda rows:rows.pop(),lambda rows:rows.reverse(),lambda rows:rows[0]['properties'].update(chain_id=999),
                lambda rows:rows[0]['properties'].update(grade=True),lambda rows:rows[0]['geometry']['coordinates'].reverse()]
            if name=='line-chains.gpkg':mutations += [lambda rows:rows[0]['properties'].update(member_count=1.0),lambda rows:rows[8]['geometry']['coordinates'][0].__setitem__(0,1)]
            else:mutations += [lambda rows:rows[0]['properties'].update(route_key='lost'),lambda rows:rows[0]['properties'].update(note=''),
                lambda rows:rows[0]['properties'].update(member_no=2),lambda rows:rows[0]['properties'].update(source_part_no=True),
                lambda rows:rows[0]['properties'].update(is_reversed=0),lambda rows:rows[0]['properties'].update(is_reversed=True)]
            for mutation in mutations:
                rows=copy.deepcopy(original);mutation(rows)
                with self.subTest(name=name,mutation=mutation),self.assertRaises(m.FixtureError):m.validate_features(name,rows)

    def test_line_component_oracle_requires_exact_membership_attributes_and_original_parts(self):
        original = m.line_component_expected_features()
        self.assertEqual(len(m.line_component_source_features()),11)
        self.assertEqual(len(original),15)
        self.assertEqual([row['properties']['component_id'] for row in original],[1,2,3,1,2,2,1,4,5,6,7,8,8,1,9])
        m.validate_line_component_features(original)
        m.validate_line_component_features(m.line_component_source_features(),source=True)
        for mutate in (
            lambda rows:rows.pop(), lambda rows:rows.reverse(),
            lambda rows:rows.__setitem__(1,copy.deepcopy(rows[0])),
            lambda rows:rows[0]['properties'].update(source_id='1'),
            lambda rows:rows[0]['properties'].update(route_key='foreign'),
            lambda rows:rows[0]['properties'].update(grade=999),
            lambda rows:rows[3]['properties'].update(note=''),
            lambda rows:rows[0]['properties'].update(part_no=True),
            lambda rows:rows[0]['properties'].update(component_id=1.0),
            lambda rows:rows[1]['properties'].update(component_id=1),
            lambda rows:rows[12]['properties'].update(component_id=9),
            lambda rows:rows[0]['geometry']['coordinates'].reverse(),
            lambda rows:rows[10]['geometry']['coordinates'][0].__setitem__(0,1),
            lambda rows:rows[-1]['geometry']['coordinates'].pop(),
        ):
            value=copy.deepcopy(original);mutate(value)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.validate_line_component_features(value)

    def test_split_line_oracle_requires_all_attributes_direction_vertices_and_ordinals(self):
        original = m.split_line_expected_features()
        self.assertEqual(len(m.split_line_source_features()),17)
        self.assertEqual(len(original),43)
        m.validate_split_line_features(original)
        m.validate_split_line_features(m.split_line_source_features(),source=True)
        self.assertIsNone(original[3]['properties']['note'])
        for mutate in (
            lambda rows: rows.pop(), lambda rows: rows.reverse(),
            lambda rows: rows.__setitem__(1,copy.deepcopy(rows[0])),
            lambda rows: rows[0]['properties'].update(source_id='1'),
            lambda rows: rows[0]['properties'].update(route_key='other'),
            lambda rows: rows[0]['properties'].update(grade=999),
            lambda rows: rows[3]['properties'].update(note=''),
            lambda rows: rows[0]['properties'].update(part_no=True),
            lambda rows: rows[0]['properties'].update(segment_no=1.0),
            lambda rows: rows[0]['properties'].pop('note'),
            lambda rows: rows[0]['geometry']['coordinates'].reverse(),
            lambda rows: rows[15]['geometry']['coordinates'][1].__setitem__(0,1e-12),
            lambda rows: rows[-1]['geometry']['coordinates'].pop(),
            lambda rows: rows[20]['geometry']['coordinates'].__setitem__(-1,[102,202]),
            lambda rows: rows.pop(34),
            lambda rows: rows[38]['properties'].update(part_no=1),
            lambda rows: rows[30]['geometry']['coordinates'].reverse(),
            lambda rows: rows[41]['geometry']['coordinates'].pop(1),
        ):
            value=copy.deepcopy(original); mutate(value)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):
                m.validate_split_line_features(value)

    def test_oracle_accepts_ring_rotation_direction_and_line_direction(self):
        faces = records('faces.geojson')
        for feature in faces:
            ring = feature['geometry']['coordinates'][0][:-1]
            ring = list(reversed(ring[2:] + ring[:2]))
            feature['geometry']['coordinates'] = [ring + [ring[0]]]
        m.validate_features('faces.geojson', faces)
        diagnostics = records('diagnostics.geojson')
        for feature in diagnostics: feature['geometry']['coordinates'].reverse()
        m.validate_features('diagnostics.geojson', diagnostics)

    def test_oracle_rejects_missing_duplicate_changed_geometry_and_business_fields(self):
        for name in m.TARGETS:
            original = records(name)
            for mutate in (lambda v: v.pop(), lambda v: v.__setitem__(0, copy.deepcopy(v[1])),
                           lambda v: v[0]['properties'].update(source_name='unexpected'),
                           lambda v: v[0]['geometry'].update(type='GeometryCollection')):
                value = copy.deepcopy(original); mutate(value)
                with self.subTest(name=name), self.assertRaises(m.FixtureError): m.validate_features(name, value)
        value = records('diagnostics.geojson'); value[0]['geometry']['coordinates'][0][0] = 118
        with self.assertRaises(m.FixtureError): m.validate_features('diagnostics.geojson', value)

    def test_repair_oracle_rejects_false_validity_dropped_parts_and_changed_attributes(self):
        m.validate_features('repaired.geojson', records('repaired.geojson'))
        for mutate in (
            lambda value: value[0]['properties'].update(geometry_valid=True),
            lambda value: value[0]['properties'].update(after_valid=False),
            lambda value: value[0]['properties'].update(after_valid=1),
            lambda value: value[0]['properties'].update(grade=8),
            lambda value: value[0]['properties'].update(geometry_reason='Valid Geometry'),
            lambda value: value[0]['geometry']['coordinates'].pop(),
            lambda value: value[0]['geometry']['coordinates'][0][0][0].__setitem__(0, 119),
        ):
            value = records('repaired.geojson'); mutate(value)
            with self.subTest(mutate=mutate), self.assertRaises(m.FixtureError):
                m.validate_features('repaired.geojson', value)

    def test_admission_rejects_personal_environment_before_system_commands(self):
        with patch.object(m, 'command') as command:
            with self.assertRaises(m.FixtureError): m.NFSExport('topology', command).guard({})
            command.assert_not_called()

    def test_difference_nfs_descriptor_has_owned_private_staging_and_cleanup(self):
        import shutil
        nfs=importlib.import_module('scripts.utils.online_nfs_fixture')
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory(prefix='addp-topology-',dir='/tmp') as source:
            secret=Path(directory).resolve();root=Path(source).resolve();exports=[];calls=[]
            def command(args,**kwargs):
                calls.append(args)
                if args==['sudo','exportfs','-s']:return ''.join(exports)
                if args[:2]==['sudo','tee']:exports[:]=[kwargs['input']];return ''
                if args[:2]==['showmount','-e']:return 'Export list:\n'+''.join(exports)
                if args[:3]==['sudo','rm','-f']:exports.clear()
                if args[:4]==['sudo','rm','-rf','--']:shutil.rmtree(args[4])
                return ''
            export=nfs.NFSExport('topology',command,publication_staging=True)
            with patch.object(nfs.tempfile,'mkdtemp',return_value=str(root)):
                export.lifecycle('start',secret,'topology-42-1')
            descriptor=json.loads((secret/'topology-nfs-engine.json').read_text())
            self.assertEqual(descriptor['connection_info'],dict(server='127.0.0.1',export_path=str(root),publication_staging_path='.addp-publication'))
            stage=root/'.addp-publication'
            self.assertEqual(stage.stat().st_mode & 0o777,0o700);self.assertEqual(stage.stat().st_uid,os.getuid())
            self.assertEqual(list(stage.iterdir()),[])
            export.lifecycle('stop',secret,'topology-42-1')
            self.assertFalse(root.exists());self.assertFalse(exports)
            self.assertFalse((secret/'topology-nfs-engine.json').exists())


    def test_private_staging_rejects_readonly_export(self):
        with self.assertRaises(m.FixtureError):m.NFSExport('topology',None,access_mode='ro',publication_staging=True)


    def test_topology_scope_cannot_clean_a_contour_or_foreign_root(self):
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory); (secret/'topology-root').write_text('/tmp/addp-contour-abcdefgh')
            with self.assertRaises(m.FixtureError): m.NFSExport('topology', m.command).owned_root(secret, 'topology-1-1')
            (secret/'topology-root').unlink(); (secret/'topology-root').symlink_to(secret/'missing')
            with self.assertRaises(m.FixtureError): m.NFSExport('topology', m.command).owned_root(secret, 'topology-1-1')

    def test_runtime_worker_refuses_a_foreign_run_before_execution(self):
        info = {'Config': {'Labels': {'com.addp.online-runtime': m.SUITE, 'com.addp.online-run-id': 'foreign'}, 'Cmd': ['sh','container_entrypoint.sh']}}
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='topology-1-1'), patch.object(m, 'command', return_value=json.dumps([info])) as command:
            with self.assertRaises(m.FixtureError): m.runtime_worker('verify', Path('/tmp/owned'))
            self.assertEqual(command.call_count, 1)

    def test_snap_oracle_rejects_chain_boundary_tie_insertions_order_and_attributes(self):
        original = records('snapped.gpkg')
        m.validate_snap_features(original)
        for mutate in (
            lambda rows: rows[0]['geometry']['coordinates'][0].__setitem__(0, 1),
            lambda rows: rows[1]['geometry']['coordinates'][0].__setitem__(0, .75),
            lambda rows: rows[2]['geometry']['coordinates'][0].__setitem__(0, .5),
            lambda rows: rows[0]['geometry']['coordinates'].insert(1, [1,0]),
            lambda rows: rows.reverse(),
            lambda rows: rows[0]['properties'].update(grade=11),
            lambda rows: rows.pop(),
        ):
            value = copy.deepcopy(original); mutate(value)
            with self.subTest(mutate=mutate), self.assertRaises(m.FixtureError): m.validate_snap_features(value)

    def test_physical_archive_retains_verified_bytes_and_refuses_changed_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);root=base/'root';root.mkdir();(root/'outputs').mkdir()
            result={'sources':{},'targets':{}}
            for name in m.SOURCES:
                (root/name).write_bytes(name.encode());result['sources'][name]=m.digest(root/name)
            for name in m.TARGETS:
                (root/'outputs'/name).write_bytes(name.encode());result['targets'][name]=m.digest(root/'outputs'/name)
            m.archive_physical_evidence(root,base/'artifacts',result)
            m.archive_physical_evidence(root,base/'artifacts',result)
            target=base/'artifacts'/'topology-physical'/'outputs'/'snapped.gpkg'
            self.assertEqual(target.read_bytes(),b'snapped.gpkg')
            target.write_bytes(b'corrupted')
            with self.assertRaises(m.FixtureError):m.archive_physical_evidence(root,base/'artifacts',result)

    def test_overlap_oracle_requires_exact_pairs_holes_parts_and_tiny_area(self):
        original=m.overlap_expected_features()
        m.validate_overlap_features(original)
        self.assertEqual(len(original),7)
        self.assertEqual(original[-1]['properties']['overlap_area'],2**-40)
        for mutate in (
            lambda rows:rows.pop(), lambda rows:rows.reverse(),
            lambda rows:rows.__setitem__(1,copy.deepcopy(rows[0])),
            lambda rows:rows[0]['properties'].update(source_id_a='共边'),
            lambda rows:rows[0]['properties'].update(source_id_b='甲'),
            lambda rows:rows[1]['properties'].update(overlap_area=True),
            lambda rows:rows[-1]['properties'].update(overlap_area=0),
            lambda rows:rows[-1]['properties'].update(overlap_area=1e-12),
            lambda rows:rows[-2]['geometry']['coordinates'].pop(),
            lambda rows:rows[0]['geometry']['coordinates'][0].append(m.rectangle(2.5,1,3,2)),
            lambda rows:rows[0]['geometry']['coordinates'][0][0][0].__setitem__(0,3),
        ):
            rows=copy.deepcopy(original);mutate(rows)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.validate_overlap_features(rows)

    def test_gap_oracle_requires_exact_regions_holes_order_and_binary_area(self):
        original=m.gap_expected_features();m.validate_gap_features(original)
        self.assertEqual(len(original),8)
        self.assertEqual([row['properties']['gap_area'] for row in original],[4,4,34,4,1,1,1,2**-40])
        for coverage in (False,True):m.validate_gap_features(m.gap_source_features(coverage),source=coverage)
        for mutate in (
            lambda rows:rows.pop(),lambda rows:rows.reverse(),
            lambda rows:rows.__setitem__(1,copy.deepcopy(rows[0])),
            lambda rows:rows[0]['properties'].update(gap_area=True),
            lambda rows:rows[-1]['properties'].update(gap_area=0),
            lambda rows:rows[-1]['properties'].update(gap_area=1e-12),
            lambda rows:rows[2]['geometry']['coordinates'].pop(),
            lambda rows:rows[2]['geometry']['coordinates'].reverse(),
            lambda rows:rows[0]['geometry']['coordinates'][0][0].__setitem__(0,101),
            lambda rows:rows.append({'properties':{'gap_area':1},'geometry':{'type':'Polygon','coordinates':[m.rectangle(190,0,191,1)]}}),
        ):
            rows=copy.deepcopy(original);mutate(rows)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.validate_gap_features(rows)
        # Hole order and ring start/direction do not change the region.
        rows=copy.deepcopy(original);rows[2]['geometry']['coordinates'][1:]=list(reversed(rows[2]['geometry']['coordinates'][1:]))
        for row in rows:
            for ring in row['geometry']['coordinates']:
                vertices=ring[:-1];vertices=list(reversed(vertices[1:]+vertices[:1]));ring[:]=vertices+[vertices[0]]
        m.validate_gap_features(rows)


    def test_dangling_oracle_checks_original_ids_order_duplicates_and_exact_points(self):
        original=m.dangling_expected_features();self.assertEqual(len(original),17)
        m.validate_dangling_features(original);m.validate_dangling_features(m.dangling_source_features(),source=True)
        self.assertEqual([row['properties']['source_id'] for row in original].count('重甲'),2)
        self.assertEqual([row['properties']['source_id'] for row in original].count('重乙'),2)
        for mutate in (
            lambda rows:rows.pop(),lambda rows:rows.reverse(),
            lambda rows:rows.__setitem__(1,copy.deepcopy(rows[0])),
            lambda rows:rows[0]['properties'].update(source_id=1),
            lambda rows:rows[0]['properties'].update(source_id='闭环'),
            lambda rows:rows[0]['properties'].update(note='copied'),
            lambda rows:rows[0]['geometry'].update(type='MultiPoint'),
            lambda rows:rows[0]['geometry']['coordinates'].append(1),
            lambda rows:rows[0]['geometry']['coordinates'].__setitem__(0,True),
            lambda rows:rows[0]['geometry']['coordinates'].__setitem__(0,float('nan')),
            lambda rows:rows[13]['geometry']['coordinates'].__setitem__(0,251),
        ):
            rows=copy.deepcopy(original);mutate(rows)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.validate_dangling_features(rows)


    def test_line_overlap_oracle_rejects_pair_source_length_part_and_order_corruption(self):
        original=m.line_overlap_expected_features()
        self.assertEqual(len(original),9);self.assertEqual(original[6]['properties']['overlap_length'],2**-40)
        m.validate_line_overlap_features(original)
        m.validate_line_overlap_features(m.line_overlap_source_features(),source=True)
        for mutate in (
            lambda rows:rows.pop(),lambda rows:rows.reverse(),
            lambda rows:rows.__setitem__(1,copy.deepcopy(rows[0])),
            lambda rows:rows[0]['properties'].update(source_id_a='接点'),
            lambda rows:rows[0]['properties'].update(source_id_b='甲'),
            lambda rows:rows[2]['properties'].update(overlap_length=True),
            lambda rows:rows[6]['properties'].update(overlap_length=0),
            lambda rows:rows[6]['properties'].update(overlap_length=1e-12),
            lambda rows:rows[5]['geometry']['coordinates'].pop(),
            lambda rows:rows[0]['geometry']['coordinates'][0][0].__setitem__(0,303),
            lambda rows:rows[7]['properties'].update(overlap_length=2),
            lambda rows:rows[0]['geometry'].update(type='LineString'),
        ):
            rows=copy.deepcopy(original);mutate(rows)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.validate_line_overlap_features(rows)
        rows=copy.deepcopy(original)
        for row in rows:
            row['geometry']['coordinates'].reverse()
            for part in row['geometry']['coordinates']:part.reverse()
        m.validate_line_overlap_features(rows)

    def test_line_self_overlap_oracle_rejects_source_counting_components_and_order_corruption(self):
        original=m.line_self_overlap_expected_features()
        self.assertEqual(len(m.line_self_overlap_source_features()),15)
        self.assertEqual(len(original),8)
        self.assertEqual(original[4]['properties']['overlap_length'],2**-40)
        m.validate_line_self_overlap_features(original)
        m.validate_line_self_overlap_features(m.line_self_overlap_source_features(),source=True)
        for kind in ('missing','extra','source','fid','length','tiny','component','coordinate','order','duplicate'):
            rows=copy.deepcopy(original)
            if kind=='missing':rows.pop()
            elif kind=='extra':rows.append(copy.deepcopy(rows[0]))
            elif kind=='source':rows[0]['properties']['source_id']='闭环'
            elif kind=='fid':rows[0]['properties']['source_id']='1'
            elif kind=='length':rows[1]['properties']['overlap_length']*=3
            elif kind=='tiny':rows[4]['properties']['overlap_length']=0
            elif kind=='component':rows[3]['geometry']['coordinates'].pop()
            elif kind=='coordinate':rows[0]['geometry']['coordinates'][0][0][0]+=.25
            elif kind=='order':rows.reverse()
            elif kind=='duplicate':rows[1]=copy.deepcopy(rows[0])
            with self.subTest(kind=kind),self.assertRaises(m.FixtureError):m.validate_line_self_overlap_features(rows)
        rows=copy.deepcopy(m.line_self_overlap_source_features())
        rows[0]['geometry']['coordinates'].pop()
        with self.assertRaises(m.FixtureError):m.validate_line_self_overlap_features(rows,source=True)


    def test_line_intersection_oracle_binds_both_original_ids_exact_points_and_order(self):
        original=m.line_intersection_expected_features()
        self.assertEqual(len(m.line_intersection_source_features()),18);self.assertEqual(len(original),10)
        m.validate_line_intersection_features(original)
        m.validate_line_intersection_features(m.line_intersection_source_features(),source=True)
        self.assertEqual([row['geometry']['coordinates'] for row in original[:3]],[[602,100]]*3)
        for kind in ('missing','extra','duplicate','order','source-a','source-b','fid','coordinate','tiny','dimension','nonfinite'):
            rows=copy.deepcopy(original)
            if kind=='missing':rows.pop()
            elif kind=='extra':rows.append(copy.deepcopy(rows[0]))
            elif kind=='duplicate':rows[1]=copy.deepcopy(rows[0])
            elif kind=='order':rows.reverse()
            elif kind=='source-a':rows[0]['properties']['source_id_a']='叠甲'
            elif kind=='source-b':rows[0]['properties']['source_id_b']='接乙'
            elif kind=='fid':rows[0]['properties']['source_id_a']='1'
            elif kind=='coordinate':rows[0]['geometry']['coordinates'][0]+=1
            elif kind=='tiny':rows[-1]['geometry']['coordinates'][0]=0
            elif kind=='dimension':rows[0]['geometry']['coordinates'].append(1)
            elif kind=='nonfinite':rows[0]['geometry']['coordinates'][0]=float('nan')
            with self.subTest(kind=kind),self.assertRaises(m.FixtureError):m.validate_line_intersection_features(rows)

    def test_line_self_intersection_oracle_binds_original_id_and_isolated_points_only(self):
        original=m.line_self_intersection_expected_features()
        self.assertEqual(len(m.line_self_intersection_source_features()),16);self.assertEqual(len(original),15)
        m.validate_line_self_intersection_features(original)
        m.validate_line_self_intersection_features(m.line_self_intersection_source_features(),source=True)
        self.assertEqual(original[9]['geometry'],original[10]['geometry'])
        self.assertNotEqual(original[9]['properties'],original[10]['properties'])
        for kind in ('missing','extra','duplicate','order','source','fid','coordinate','tiny','dimension','nonfinite','overlap-point','seam','global-dedup'):
            rows=copy.deepcopy(original)
            if kind=='missing':rows.pop()
            elif kind=='extra':rows.append(copy.deepcopy(rows[0]))
            elif kind=='duplicate':rows[1]=copy.deepcopy(rows[0])
            elif kind=='order':rows.reverse()
            elif kind=='source':rows[0]['properties']['source_id']='重复'
            elif kind=='fid':rows[0]['properties']['source_id']='1'
            elif kind=='coordinate':rows[0]['geometry']['coordinates'][0]+=1
            elif kind=='tiny':rows[8]['geometry']['coordinates'][0]=0
            elif kind=='dimension':rows[0]['geometry']['coordinates'].append(1)
            elif kind=='nonfinite':rows[0]['geometry']['coordinates'][0]=float('nan')
            elif kind=='overlap-point':rows.append({'properties':{'source_id':'叠支'},'geometry':{'type':'Point','coordinates':[902,140]}})
            elif kind=='seam':rows.append({'properties':{'source_id':'环缝'},'geometry':{'type':'Point','coordinates':[870,140]}})
            elif kind=='global-dedup':rows.pop(10)
            with self.subTest(kind=kind),self.assertRaises(m.FixtureError):m.validate_line_self_intersection_features(rows)


class NormalizeFixtureTest(unittest.TestCase):
    def test_case_selection_refuses_unknown_before_old_worker(self):
        self.assertEqual(m.topology_case({}), 'full')
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.NORMALIZE_CASE}),m.NORMALIZE_CASE)
        for value in ('', 'normal', 'FULL'):
            with self.assertRaises(m.FixtureError):m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':value})
        with patch.object(m,'normalize_worker',return_value={'only':'normalize'}) as normalize,patch.object(m,'seed_snap',side_effect=AssertionError('old scenario must not run')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.NORMALIZE_CASE),{'only':'normalize'})
            normalize.assert_called_once()

    def test_exact_normalized_edges_and_every_original_occurrence_are_independent(self):
        self.assertEqual(len(m.normalize_source_features()),5)
        edges=m.normalize_expected_features();members=m.normalize_expected_features('members')
        self.assertEqual(len(edges),9);self.assertEqual(len(members),17)
        self.assertEqual([r['properties']['occurrence_count'] for r in edges],[1,2,2,1,1,1,1,4,4])
        self.assertEqual(sum(r['properties']['occurrence_count'] for r in edges),len(members))
        self.assertEqual(members[4]['properties']['source_segment_no'],3)
        self.assertEqual([r['properties']['source_id'] for r in members].count('重复丁'),6)
        for kind,original in [('source',m.normalize_source_features()),('result',edges),('members',members)]:
            m.validate_normalize_features(original,kind)
            changes=[lambda rows:rows.pop(),lambda rows:rows.reverse(),lambda rows:rows[0]['geometry']['coordinates'].reverse()]
            for field in original[0]['properties']:
                changes.append(lambda rows,field=field:rows[0]['properties'].__setitem__(field,'wrong'))
            if kind=='members':
                changes.extend([lambda rows:rows[0]['properties'].update(is_reversed=0),lambda rows:rows[5]['properties'].update(grade=0),
                    lambda rows:rows[4]['properties'].update(source_segment_no=2),lambda rows:rows[0]['properties'].update(edge_id=True)])
            for change in changes:
                rows=copy.deepcopy(original);change(rows)
                with self.subTest(kind=kind,change=change),self.assertRaises(m.FixtureError):m.validate_normalize_features(rows,kind)


class CompositionFixtureTest(unittest.TestCase):
    def test_composition_counts_types_and_source_unchanged(self):
        original=m.composition_source_features();self.assertEqual(len(original),11)
        self.assertTrue(all(type(r['properties']['route_key']) is int and 2**32<r['properties']['route_key']<2**53 for r in original))
        tables={p:m.composition_expected_features(p) for p in m.COMPOSITION_PORTS}
        self.assertEqual([len(tables[p]) for p in m.COMPOSITION_PORTS],[9,15,22]);m.verify_composition_relations(tables)
        self.assertEqual(original,m.composition_source_features())
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.COMPOSITION_CASE}),m.COMPOSITION_CASE)
        with patch.object(m,'normalize_worker',return_value={'only':'composition'}) as worker:
            self.assertEqual(m.worker('seed',Path('/unused'),m.COMPOSITION_CASE),{'only':'composition'})
            worker.assert_called_once_with('seed',Path('/unused'),m.COMPOSITION_CASE)
    def test_composition_oracle_rejects_missing_duplicate_type_provenance_geometry_and_field_scope(self):
        for port in m.COMPOSITION_PORTS:
            original=m.composition_expected_features(port);m.validate_normalize_features(original,port,m.COMPOSITION_CASE)
            changes=[lambda rows:rows.pop(),lambda rows:rows.reverse(),lambda rows:rows.append(copy.deepcopy(rows[0])),
                lambda rows:rows[0]['geometry']['coordinates'].reverse(),lambda rows:rows[0]['geometry']['coordinates'][0].__setitem__(0,999)]
            for key,value in original[0]['properties'].items():
                changes.append(lambda rows,key=key:rows[0]['properties'].__setitem__(key,'wrong'))
                if type(value) in (bool,int):changes.append(lambda rows,key=key,value=value:rows[0]['properties'].__setitem__(key,int(value) if type(value)is bool else float(value)))
            for change in changes:
                altered=copy.deepcopy(original);change(altered)
                with self.subTest(port=port,change=change),self.assertRaises(m.FixtureError):m.validate_normalize_features(altered,port,m.COMPOSITION_CASE)
    def test_composition_relation_checker_rejects_lost_chain_member_occurrence_or_direction(self):
        mutations=[lambda t:t['chains'][0]['properties'].update(member_count=999),
            lambda t:t['chain_edges'][0]['properties'].update(occurrence_count=999),
            lambda t:t['chain_edges'][0]['properties'].update(is_reversed=True),
            lambda t:t['edge_sources'][0]['properties'].update(is_reversed=True),
            lambda t:t['edge_sources'][0]['properties'].update(chain_id=1)]
        for mutate in mutations:
            tables={p:m.composition_expected_features(p) for p in m.COMPOSITION_PORTS};mutate(tables)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.verify_composition_relations(tables)


class BufferFixtureTest(unittest.TestCase):
    def test_case_routes_only_to_two_source_buffer_fixture_and_keeps_exact_distance_order(self):
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.BUFFER_CASE}),m.BUFFER_CASE)
        with patch.object(m,'buffer_worker',return_value={'case':'buffer'}) as selected,patch.object(m,'normalize_worker',side_effect=AssertionError('old case must not run')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.BUFFER_CASE),{'case':'buffer'})
            selected.assert_called_once_with('seed',Path('/unused'))
        expected=m.buffer_features('result')
        self.assertEqual([r['properties']['distance'] for r in expected],[2.,2.,1.,1.,2.,2.])
        self.assertEqual([r['properties']['route_key'] for r in expected],[2**40+1,2**40+2]*3)
        self.assertEqual(expected[0],expected[4]);self.assertEqual(expected[1],expected[5])
        self.assertEqual([r['properties']['distance'] for r in m.buffer_features('conflict')],[99.,99.])


class ProximityFixtureTest(unittest.TestCase):
    def test_proximity_hand_oracle_has_exact_pairs_types_original_points_and_strict_boundaries(self):
        source=m.proximity_features('source');reference=m.proximity_features('reference');rows=m.proximity_features('result')
        self.assertEqual((len(source),len(reference),len(rows)),(5,9,7))
        self.assertEqual([r['properties']['reference_id'] for r in rows],['z-east','a-west','m-parts','b-east-copy','micro','shared','shared'])
        self.assertEqual([r['properties']['distance'] for r in rows],[.5,.5,.25,.5,2**-20,.5,.5])
        self.assertEqual(rows[2]['properties']['nearest_x'],-.25)
        self.assertEqual(len({tuple(r['geometry']['coordinates']) for r in rows}),3)
        self.assertTrue(all(r['properties']['source_id']>2**32 and r['properties']['source_id']<2**53 for r in rows))
        self.assertNotIn(2**40+7,[r['properties']['source_id'] for r in rows])
        m.validate_proximity_features(rows)
    def test_proximity_oracle_rejects_drop_duplicate_id_coercion_nearest_instead_of_source_and_sorting(self):
        original=m.proximity_features('result')
        for mutate in [lambda r:r.pop(),lambda r:r.append(copy.deepcopy(r[0])),lambda r:r.reverse(),
            lambda r:r[0]['properties'].update(source_id=float(2**40+9)),lambda r:r[0]['properties'].update(reference_id='other'),
            lambda r:r[0]['properties'].update(distance=1.),lambda r:r[2]['properties'].update(nearest_x=.25),
            lambda r:r[0]['geometry'].update(coordinates=[.5,0]),lambda r:r[0]['properties'].update(note='copied'),
            lambda r:r[4]['properties'].update(distance=0.)]:
            rows=copy.deepcopy(original);mutate(rows)
            with self.subTest(mutate=mutate),self.assertRaises(m.FixtureError):m.validate_proximity_features(rows)


class TrimFixtureTest(unittest.TestCase):
    def test_trim_selected_worker_never_runs_full_or_old_cases(self):
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.TRIM_CASE}),m.TRIM_CASE)
        self.assertEqual(m.network_profile(m.TRIM_CASE)['title'],m.TRIM_TITLE)
        with patch.object(m,'trim_worker',return_value={'only':'trim'}) as worker,patch.object(m,'seed_snap',side_effect=AssertionError('old scenario')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.TRIM_CASE),{'only':'trim'});worker.assert_called_once()
    def test_trim_frozen_expectation_rejects_missing_source_attribute_original_point_or_path_distance(self):
        self.assertEqual([len(m.trim_features(k)) for k in ('source','reference','result','details')],[15,14,15,9])
        self.assertEqual(sum(len(r['geometry']['coordinates']) for r in m.trim_features('source')),16)
        for kind,change in [('result',lambda r:r.reverse()),('result',lambda r:r[0]['properties'].pop('geometry')),
                ('details',lambda r:r[0]['geometry'].update(coordinates=[0,0])),('details',lambda r:r[3]['properties'].update(distance=2**.5)),
                ('details',lambda r:r.pop(1)),('details',lambda r:r[0]['properties'].update(part_no=True))]:
            rows=copy.deepcopy(m.trim_features(kind));change(rows)
            with self.subTest(kind=kind),self.assertRaises(m.FixtureError):m.validate_extension_features(rows,kind,m.TRIM_CASE)


class NodesFixtureTest(unittest.TestCase):
    def test_nodes_selected_worker_never_runs_full_or_old_cases(self):
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.NODES_CASE}),m.NODES_CASE)
        self.assertEqual(m.network_profile(m.NODES_CASE)['title'],m.NODES_TITLE)
        with patch.object(m,'nodes_worker',return_value={'only':'nodes'}) as worker,patch.object(m,'seed_snap',side_effect=AssertionError('old scenario')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.NODES_CASE),{'only':'nodes'});worker.assert_called_once()
    def test_nodes_handwritten_degrees_and_complete_repeated_incidence(self):
        nodes=m.nodes_features('result');members=m.nodes_features('members')
        self.assertEqual([len(m.nodes_features('source')),len(nodes),len(members)],[9,15,22])
        self.assertEqual(sum(r['properties']['degree'] for r in nodes),22)
        self.assertEqual([r['properties']['end'] for r in members[:2]],['start','end'])
        self.assertEqual([r['properties']['node_id'] for r in members[:3]],[1,1,1])
        self.assertEqual(nodes[12]['geometry']['coordinates'],[41,0]);self.assertEqual(nodes[13]['geometry']['coordinates'],[41+2**-40,0])
        self.assertEqual(len(m.NODES_FAILURES),9)


class OverlayFixtureTest(unittest.TestCase):
    def test_overlay_frozen_pairs_cover_all_dimensions_collections_duplicates_and_tiny_area(self):
        a,b,result=m.overlay_features('a'),m.overlay_features('b'),m.overlay_features('result')
        self.assertEqual([len(m.overlay_features(k)) for k in (*m.OVERLAY_KINDS,'result')],[17,18,18,18,16])
        self.assertEqual({r['geometry']['type'] for r in result},{'Point','LineString','Polygon','MultiPoint','MultiLineString','MultiPolygon','GeometryCollection'})
        self.assertEqual(result[0]['geometry'],result[1]['geometry'])
        self.assertEqual(result[0]['properties']['source_id_a'],result[1]['properties']['source_id_a'])
        self.assertNotEqual(result[0]['properties']['source_id_b'],result[1]['properties']['source_id_b'])
        self.assertEqual([g['type'] for g in result[13]['geometry']['geometries']],['Polygon','LineString','Point'])
        ring=result[-1]['geometry']['coordinates'][0]
        self.assertEqual((ring[1][0]-ring[0][0])*(ring[2][1]-ring[1][1]),2**-40)
        self.assertFalse({a[15]['properties']['a_key'],a[16]['properties']['a_key']}&{r['properties']['source_id_a'] for r in result})
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':'intersection-overlay'}),m.OVERLAY_CASE)
        self.assertEqual(m.network_profile(m.OVERLAY_CASE)['targets'],('intersection.gpkg',))
    def test_overlay_preserves_original_ids_nullable_property_types_and_separate_geometry_identity(self):
        result=m.overlay_features('result')
        self.assertTrue(all(type(r['properties']['source_id_a']) is int and 2**40<r['properties']['source_id_a']<2**53 for r in result))
        self.assertTrue(all(type(r['properties']['source_id_b']) is str for r in result))
        self.assertTrue(any(r['properties']['a_grade'] is None for r in result))
        self.assertTrue(any(r['properties']['a_enabled'] is None for r in result))
        self.assertTrue(any(r['properties']['geometry'] is None for r in result))
        self.assertTrue(all(r['properties']['a_enabled'] is None or type(r['properties']['a_enabled']) is bool for r in result))
        self.assertEqual(m.overlay_features('fields')[0]['properties']['a_note'],m.overlay_features('b')[0]['properties']['b_note'])


class DissolveFixtureTest(unittest.TestCase):
    def test_dissolve_groups_global_and_complete_null_members_are_handwritten(self):
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':'polygon-dissolve'}),m.DISSOLVE_CASE)
        self.assertEqual(m.network_profile(m.DISSOLVE_CASE)['targets'],m.DISSOLVE_TARGETS)
        self.assertEqual([len(m.dissolve_features(k))for k in('source','grouped-result','grouped-members','global-result','global-members')],[10,4,10,1,10])
        self.assertEqual([r['properties']['member_count']for r in m.dissolve_features('grouped-result')],[4,2,2,2])
        self.assertEqual([r['properties']['fusion_id']for r in m.dissolve_features('grouped-members')],[1,2,2,1,1,3,3,4,4,1])
        self.assertEqual([r['properties']['source_id']for r in m.dissolve_features('global-members')],[r['properties']['parcel_key']for r in m.dissolve_features('source')])
        self.assertEqual(len(m.DISSOLVE_FAILURES),8)
        self.assertEqual({(s[2],s[3])for s in m.DISSOLVE_FAILURES},{('operator_input_invalid','input_gdf'),('operator_crs_invalid',''),('operator_group_fields_invalid','group_fields'),('operator_dissolve_fields_conflict','')})
    def test_dissolve_dispatch_never_calls_older_business(self):
        with patch.object(m,'dissolve_worker',return_value={'only':'dissolve'})as worker,patch.object(m,'overlay_worker',side_effect=AssertionError('old business')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.DISSOLVE_CASE),{'only':'dissolve'});worker.assert_called_once()


class DifferenceFixtureTest(unittest.TestCase):
    def test_difference_handwritten_source_selection_whole_mask_and_native_shapes(self):
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':'geometry-difference'}),m.DIFFERENCE_CASE)
        self.assertEqual(m.network_profile(m.DIFFERENCE_CASE)['targets'],(m.DIFFERENCE_TARGET,))
        self.assertEqual([len(m.difference_features(k))for k in(*m.DIFFERENCE_KINDS,'result')],[12,3,12,3,3,9])
        rows=m.difference_features('result');self.assertEqual([r['properties']['order_tag']for r in rows],[1,4,5,7,8,9,10,11,12])
        self.assertEqual({r['geometry']['type']for r in rows},{'Point','MultiPoint','LineString','MultiLineString','Polygon','MultiPolygon','GeometryCollection'})
        self.assertEqual(rows[2]['geometry']['coordinates'],[[[400,0],[401,0]],[[404,0],[406,0]]])
        self.assertEqual(len(rows[7]['geometry']['geometries']),6)
        self.assertEqual(len(m.DIFFERENCE_FAILURES),4)
        self.assertTrue(all('source_id'not in row['properties']for row in rows))
        self.assertEqual(m.difference_features('b')[1]['properties']['a_key'],m.difference_features('b')[2]['properties']['a_key'])
    def test_difference_dispatch_is_independent_and_never_runs_old_overlay(self):
        with patch.object(m,'difference_worker',return_value={'only':'difference'})as worker,patch.object(m,'overlay_worker',side_effect=AssertionError('old business')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.DIFFERENCE_CASE),{'only':'difference'});worker.assert_called_once()


class VerticesFixtureTest(unittest.TestCase):
    def test_vertices_literal_occurrences_and_numbering_do_not_deduplicate(self):
        self.assertEqual([len(m.vertices_features(k+'-result')) for k in m.VERTICES_FAMILIES],[3,8,20])
        for family in m.VERTICES_FAMILIES:
            rows=m.vertices_features(family+'-result');self.assertEqual([r['properties']['vertex_id'] for r in rows],list(range(1,len(rows)+1)))
            for change in [lambda r:r.pop(),lambda r:r.reverse(),lambda r:r[0]['properties'].update(vertex_id=True),lambda r:r[0]['properties'].update(note='lost'),lambda r:r[0]['geometry']['coordinates'].__setitem__(0,99)]:
                value=copy.deepcopy(rows);change(value)
                with self.subTest(family=family,change=change),self.assertRaises(m.FixtureError):m.validate_vertices_features(value,family+'-result')
        line=m.vertices_features('line-result');self.assertEqual(line[0]['geometry'],line[2]['geometry']);self.assertNotEqual(line[0]['properties']['vertex_no'],line[2]['properties']['vertex_no'])
        self.assertEqual(line[3]['geometry'],line[4]['geometry']);self.assertEqual([v['properties']['ring_no'] for v in m.vertices_features('polygon-result')],[1]*5+[2]*5+[1]*10)
    def test_vertices_case_dispatch_uses_only_owned_fixture_and_inventory(self):
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.VERTICES_CASE}),m.VERTICES_CASE)
        with patch.object(m,'vertices_worker',return_value={'owned':True}) as worker,patch.object(m,'seed_snap',side_effect=AssertionError('old scenario')):
            self.assertEqual(m.worker('seed',Path('/unused'),m.VERTICES_CASE),{'owned':True});worker.assert_called_once_with('seed',Path('/unused'))


class AdjacencyFixtureTest(unittest.TestCase):
    def test_adjacency_handwritten_five_pairs_tiny_edge_and_exact_source_inventory(self):
        self.assertEqual(m.network_profile(m.ADJACENCY_CASE)['sources'],m.ADJACENCY_SOURCES)
        self.assertEqual([len(m.adjacency_features(k))for k in m.ADJACENCY_KINDS],[12,12,12,12,3])
        result=m.adjacency_features('result')
        self.assertEqual([(r['properties']['source_id_a'],r['properties']['source_id_b'],r['properties']['shared_length'])for r in result],[(90,10,2.),(10,70,2.),(40,30,8.),(60,20,3.),(80,50,2**-40)])
        self.assertEqual(len(result[3]['geometry']['coordinates']),2)
        self.assertEqual(result[4]['geometry']['coordinates'],[[[41,0],[41,2**-40]]])
        self.assertEqual(m.adjacency_features('empty')[1]['geometry']['coordinates'],[])
        self.assertEqual(m.adjacency_features('source')[9]['geometry']['coordinates'][0][0],[32+2**-40,1])
        self.assertEqual(m.topology_case({'ADDP_ONLINE_TOPOLOGY_CASE':m.ADJACENCY_CASE}),m.ADJACENCY_CASE)
