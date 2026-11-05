"""Member isolation and fail-closed explicit migration; never uses real Docker."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import Mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('metrics_project_test',ROOT/'scripts/utils/metrics-project.py')
M = importlib.util.module_from_spec(spec); spec.loader.exec_module(M)


class ProjectTest(unittest.TestCase):
    def test_fixed_project_and_unique_services(self):
        self.assertEqual(M.PROJECT, 'addp-metrics-sources')
        members = [M.service(k,s) for k in ('postgres','redis','minio') for s in ('infra','business')]
        self.assertEqual(len(set(members)),6)
        self.assertNotIn(M.service('node','host'),members)
        for k,s in [('node','infra'),('postgres','host'),('unknown','business')]:
            with self.assertRaises(ValueError):M.service(k,s)

    def test_queries_select_each_service_separately_and_union_ids(self):
        calls=[]
        def run(args):
            calls.append(args)
            return 'source' if args[-1].endswith('infra-postgres-metrics') else 'gateway'
        self.assertEqual(M.container_ids(run,'one',{'infra-postgres-metrics','infra-postgres-metrics-web'}),['gateway','source'])
        self.assertEqual(len(calls),2)
        for args in calls:
            self.assertIn('label=com.docker.compose.project=one',args)
            self.assertEqual(sum(a.startswith('label=com.docker.compose.service=') for a in args),1)
        with self.assertRaises(ValueError):M.filters('one',{'a','b'})

    def fixture(self,kind='minio'):
        project=M.previous_project(ROOT);namespace='io.addp.'+kind+'-metrics'
        fact={'labels':{'com.docker.compose.project':project,'com.docker.compose.service':M.service(kind,'infra'),
            'com.docker.compose.oneoff':'False',namespace+'.owner':str(ROOT),namespace+'.scope':'infra'}}
        volume={'Name':project+'-infra-'+kind+'-private','Driver':'local','Labels':{namespace+'.owner':str(ROOT),namespace+'.scope':'infra'}}
        commands=[];docker=Mock();docker.fact.side_effect=lambda _:copy.deepcopy(fact)
        def run(args,**kw):
            commands.append(args)
            if args[0]=='ps':
                return 'a'*64 if args[-1].endswith('='+M.service(kind,'infra')) else ''
            if args[:2]==['volume','ls']:return volume['Name']
            if args[:2]==['volume','inspect']:return json.dumps([volume])
            if args[0]=='rm' or args[:2]==['volume','rm']:return ''
            raise AssertionError(args)
        docker.run.side_effect=run
        return docker,fact,volume,commands

    def test_migration_only_removes_verified_old_member_and_private_volume(self):
        docker,fact,volume,commands=self.fixture()
        M.retire(docker,ROOT,'minio','infra')
        self.assertEqual([c for c in commands if c[0]=='rm' or c[:2]==['volume','rm']],
            [['rm','-f','a'*64],['volume','rm',volume['Name']]])
        self.assertIn(['ps','-aq','--no-trunc',*M.filters(M.previous_project(ROOT),{'infra-minio-metrics'})], commands)
        self.assertFalse(any(c[0] in ('compose','start','build','pull','exec') for c in commands))

    def test_postgres_retirement_preserves_other_members_of_previous_group(self):
        old = M.previous_project(ROOT)
        exporter = M.service('postgres', 'infra')
        objects = {'exporter': exporter, 'web': exporter+'-web',
                   'peer': M.service('postgres', 'business'), 'node': 'node-exporter'}
        commands = []
        docker = Mock()
        docker.fact.side_effect = lambda identity: {'labels': {
            'com.docker.compose.project': old, 'com.docker.compose.service': objects[identity],
            'com.docker.compose.oneoff': 'False', 'io.addp.postgres-metrics.owner': str(ROOT),
            'io.addp.postgres-metrics.scope': 'infra'}}
        def run(args, **kwargs):
            commands.append(args)
            if args[0] == 'ps':
                self.assertIn('label=com.docker.compose.project='+old, args)
                return '\n'.join(identity for identity, service in objects.items()
                                 if 'label=com.docker.compose.service='+service in args)
            if args[0] == 'rm':
                objects.pop(args[-1])
                return ''
            raise AssertionError(args)
        docker.run.side_effect = run
        M.retire(docker, ROOT, 'postgres', 'infra')
        self.assertEqual(list(objects), ['peer', 'node'])
        self.assertEqual([args[-1] for args in commands if args[0] == 'rm'], ['web', 'exporter'])

    def test_foreign_container_or_volume_prevents_all_removal(self):
        for target,key in [('container','io.addp.minio-metrics.owner'),('container','io.addp.minio-metrics.scope'),
                           ('container','com.docker.compose.service'),('container','com.docker.compose.project'),
                           ('volume','io.addp.minio-metrics.owner'),('volume','io.addp.minio-metrics.scope')]:
            with self.subTest(target=target,key=key):
                docker,fact,volume,commands=self.fixture()
                (fact['labels'] if target=='container' else volume['Labels'])[key]='foreign'
                with self.assertRaises(ValueError):M.retire(docker,ROOT,'minio','infra')
                self.assertFalse(any(c[0]=='rm' or c[:2]==['volume','rm'] for c in commands))

    def test_single_source_stop_preserves_other_members_in_same_project(self):
        spec=importlib.util.spec_from_file_location('project_source_stop',ROOT/'scripts/utils/metrics-source.py')
        source_module=importlib.util.module_from_spec(spec);spec.loader.exec_module(source_module)
        docker=Mock();source=source_module.Source(docker,'infra',Path('/unused'),ROOT,'minio','shared')
        objects={'mine':{'labels':{source.owner:str(ROOT),source.scope_label:'infra',
            'com.docker.compose.project':'shared','com.docker.compose.service':source.service}},
            'peer':{'labels':{'com.docker.compose.project':'shared','com.docker.compose.service':'business-minio-metrics'}}}
        commands=[]
        def run(args,**kw):
            commands.append(args)
            if args[0]=='ps':
                # The real Docker label filter intersection, not a mocked OR.
                terms=[a.removeprefix('label=').split('=',1) for a in args if a.startswith('label=')]
                return '\n'.join(k for k,v in objects.items() if all(v['labels'].get(a)==b for a,b in terms))
            if args[:2]==['volume','ls']:return ''
            if args[0]=='rm':objects.pop(args[-1]);return ''
            raise AssertionError(args)
        docker.run.side_effect=run;docker.fact.side_effect=lambda key:copy.deepcopy(objects[key])
        source.stop()
        self.assertEqual(set(objects),{'peer'})
        self.assertEqual([c for c in commands if c[0]=='rm'],[['rm','-f','mine']])

    def test_node_retirement_rejects_a_foreign_network_attachment_before_removal(self):
        docker=Mock();old=M.previous_project(ROOT);commands=[]
        labels={'com.docker.compose.project':old,'com.docker.compose.service':'node-exporter',
            'com.docker.compose.oneoff':'False','io.addp.node-metrics.owner':str(ROOT)}
        docker.fact.return_value={'labels':labels}
        def run(args,**kw):
            commands.append(args)
            if args[0]=='ps':return 'a'*64 if args[-1].endswith('=node-exporter') else ''
            if args[:2]==['volume','ls']:return ''
            if args[:2]==['network','ls']:return 'n'
            if args[:2]==['network','inspect']:return json.dumps([{'Name':old+'_default','Driver':'bridge',
                'Labels':{'com.docker.compose.project':old,'com.docker.compose.network':'default','io.addp.node-metrics.owner':str(ROOT)},'Containers':{'foreign':{}}}])
            raise AssertionError(args)
        docker.run.side_effect=run
        with self.assertRaises(ValueError):M.retire(docker,ROOT,'node','host')
        self.assertFalse(any(c[0]=='rm' or c[:2] in (['volume','rm'],['network','rm']) for c in commands))


if __name__=='__main__':unittest.main()
