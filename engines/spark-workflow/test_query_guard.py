"""Cheap fail-closed boundary tests; syntax is verified by the native T2 gate."""
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from query_guard import QueryValidationError, validate_query


class QueryGuardTest(unittest.TestCase):
    def session(self, root=None):
        parser = Mock()
        parser.parsePlan.return_value = root
        session = SimpleNamespace(version='3.5.0', _jsparkSession=Mock(), sql=Mock())
        session._jsparkSession.sessionState.return_value.sqlParser.return_value = parser
        return session, parser

    def test_unsupported_version_and_invalid_requests_never_invoke_parser(self):
        for version, query, catalogs in (
            ('3.5.1', 'SELECT 1', {'pg'}), ('3.5.0', '', {'pg'}),
            ('3.5.0', 'SELECT 1', set()), ('3.5.0', 'SELECT 1', ['pg']),
            ('3.5.0', 'SELECT 1', {'pg.public'}),
            ('3.5.0', 'SELECT 1', {' pg'}), ('3.5.0', 'x' * (256 * 1024 + 1), {'pg'}),
        ):
            with self.subTest(version=version, catalogs=catalogs):
                session, parser = self.session()
                session.version = version
                with self.assertRaises(QueryValidationError):
                    validate_query(session, query, catalogs)
                parser.parsePlan.assert_not_called()
                session.sql.assert_not_called()

    def test_native_parser_errors_do_not_escape_with_raw_sql_or_credentials(self):
        session, parser = self.session()
        parser.parsePlan.side_effect = ValueError('private-credential-and-query')
        with self.assertRaisesRegex(QueryValidationError, '^query_invalid$'):
            validate_query(session, 'SELECT 1', {'pg'})
        session.sql.assert_not_called()

    def test_unknown_native_classes_are_rejected_before_any_child_is_visited(self):
        root = Mock()
        root.getClass.return_value.getName.return_value = 'custom.Project'
        session, _ = self.session(root)
        with self.assertRaisesRegex(QueryValidationError, '^query_not_read_only$'):
            validate_query(session, 'SELECT 1', {'pg'})
        root.children.assert_not_called()
        root.expressions.assert_not_called()
        session.sql.assert_not_called()


if __name__ == '__main__':
    unittest.main()
