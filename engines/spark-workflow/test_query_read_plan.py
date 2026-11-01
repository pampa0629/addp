"""Metadata proof and canonical physical names, without executing user SQL."""
import unittest
from unittest.mock import Mock, patch

from query_read_plan import lock_read_plan, _hold


class QueryReadPlanTest(unittest.TestCase):
    def test_all_relations_must_be_proved_and_canonical_names_come_from_catalog(self):
        connection = Mock()
        source = {'alias':'mysql', 'engine_id':12,'engine_type':'mysql', 'url':'jdbc:mysql://db/data','user':'reader','password':'private'}
        spark = Mock()
        spark._jvm.com.mysql.cj.jdbc.Driver.return_value.connect.return_value = connection
        with patch('query_read_plan._relation', return_value=('RealDB','ActualTable')) as relation, patch('query_read_plan._hold') as hold:
            with lock_read_plan(spark, [source], {('mysql','realdb','actualtable')}) as facts:
                self.assertEqual(facts['engine_read_scope'], {'engine_ids':[12]})
                path = facts['read_set']['paths'][0]
                self.assertEqual([p.get('name') for p in path['segments'][1:]], ['RealDB','ActualTable'])
                connection.close.assert_not_called()
            hold.assert_called_once_with(connection, source, 'RealDB','ActualTable')
            connection.rollback.assert_called_once()
            connection.close.assert_called_once()

    def test_any_unknown_relation_invalidates_the_whole_scope_and_read_set(self):
        source = {'alias':'mysql','engine_id':12,'engine_type':'mysql','url':'jdbc:mysql://db/data','user':'reader','password':'private'}
        spark = Mock()
        with patch('query_read_plan._relation', side_effect=[('db','ordinary'),('db','ordinary'),None]), patch('query_read_plan._hold'):
            with lock_read_plan(spark,[source],{('mysql','db','ordinary'),('mysql','db','view')}) as facts:
                self.assertIsNone(facts['engine_read_scope'])
                self.assertIsNone(facts['read_set'])

    def test_read_set_deduplicates_canonical_names_after_native_resolution(self):
        source = {'alias':'mysql','engine_id':12,'engine_type':'mysql','url':'jdbc:mysql://db/data','user':'reader','password':'private'}
        spark = Mock()
        with patch('query_read_plan._relation', return_value=('ActualDB','ActualTable')), patch('query_read_plan._hold'):
            with lock_read_plan(spark,[source],{('mysql','actualdb','actualtable'),('mysql','ActualDB','ActualTable')}) as facts:
                self.assertEqual(len(facts['read_set']['paths']), 1)
                self.assertEqual(facts['engine_read_scope'], {'engine_ids':[12]})

    def test_postgres_without_enforced_only_catalog_remains_unknown(self):
        source = {'alias':'pg','engine_id':11,'engine_type':'postgresql','url':'jdbc:postgresql://db/data','user':'reader','password':'private'}
        spark = Mock()
        spark.conf.get.return_value = 'org.apache.spark.sql.execution.datasources.v2.jdbc.JDBCTableCatalog'
        with patch('query_read_plan._relation', return_value=('public','ordinary')), patch('query_read_plan._hold'):
            with lock_read_plan(spark,[source],{('pg','public','ordinary')}) as facts:
                self.assertIsNone(facts['engine_read_scope'])
                self.assertIsNone(facts['read_set'])

    def test_postgres_requires_both_only_catalog_and_held_local_table_proof(self):
        source = {'alias':'pg','engine_id':11,'engine_type':'postgresql','url':'jdbc:postgresql://db/data','user':'reader','password':'private'}
        spark = Mock()
        spark.conf.get.return_value = 'com.addp.spark.query.ExactIdentifierDialects$OnlyPostgresCatalog'
        with patch('query_read_plan._relation', return_value=('public','ordinary')), patch('query_read_plan._hold'):
            with lock_read_plan(spark,[source],{('pg','public','ordinary')}) as facts:
                self.assertEqual(facts['engine_read_scope'], {'engine_ids':[11]})
                self.assertEqual(facts['read_set']['paths'][0]['segments'][2]['name'], 'ordinary')

    def test_identifier_quotes_are_escaped_without_trimming_and_no_rows_are_read(self):
        for engine, schema, table, expected in [
            ('postgresql',' schema." ',' table.name ','LOCK TABLE ONLY " schema."" "." table.name " IN ACCESS SHARE MODE'),
            ('mysql','db`',' table` ','SELECT * FROM `db``` .` table`` ` WHERE 1=0'.replace('` .','`.')),
        ]:
            with self.subTest(engine=engine):
                connection = Mock()
                _hold(connection, {'engine_type':engine}, schema, table)
                statement = connection.createStatement.return_value
                statement.setQueryTimeout.assert_called_once_with(10)
                if engine=='postgresql': statement.execute.assert_called_once_with(expected)
                else: statement.executeQuery.assert_called_once_with(expected)
                statement.close.assert_called_once()
