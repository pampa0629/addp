package com.addp.spark.query;

import java.util.Collections;
import java.util.Map;
import java.util.HashMap;
import scala.Tuple2;
import scala.collection.JavaConverters;
import org.apache.spark.sql.connector.catalog.Identifier;
import org.apache.spark.sql.connector.catalog.Table;
import org.apache.spark.sql.execution.datasources.jdbc.JDBCOptions;
import org.apache.spark.sql.execution.datasources.jdbc.JDBCRDD;
import org.apache.spark.sql.execution.datasources.v2.jdbc.JDBCTable;
import org.apache.spark.sql.execution.datasources.v2.jdbc.JDBCTableCatalog;
import org.apache.spark.sql.util.CaseInsensitiveStringMap;
import org.apache.spark.SparkContext;
import org.apache.spark.api.plugin.DriverPlugin;
import org.apache.spark.api.plugin.ExecutorPlugin;
import org.apache.spark.api.plugin.PluginContext;
import org.apache.spark.api.plugin.SparkPlugin;
import org.apache.spark.sql.jdbc.JdbcDialect;
import org.apache.spark.sql.jdbc.JdbcDialects;
import org.apache.spark.sql.jdbc.JdbcSQLQueryBuilder;
import org.apache.spark.sql.jdbc.JdbcType;
import org.apache.spark.sql.jdbc.MySQLDialect;
import org.apache.spark.sql.jdbc.MySQLDialect$;
import org.apache.spark.sql.jdbc.PostgresDialect$;
import org.apache.spark.sql.connector.expressions.Expression;
import org.apache.spark.sql.connector.expressions.NamedReference;
import org.apache.spark.sql.types.DataType;
import org.apache.spark.sql.types.MetadataBuilder;
import scala.Option;

/** Read-only query driver extension for Spark 3.5.0 identifier escaping.
 * A single adapter retains native read semantics and corrects identifier
 * quoting, without AggregatedDialect losing native SQL builders. The PG catalog bounds
 * native scans to the exact ordinary table; no new write route is exposed.
 */
public final class ExactIdentifierDialects implements SparkPlugin {
    public ExactIdentifierDialects() {}
    @Override public DriverPlugin driverPlugin() {
        return new DriverPlugin() {
            @Override public Map<String, String> init(SparkContext sc, PluginContext context) {
                install();
                return Collections.emptyMap();
            }
        };
    }
    @Override public ExecutorPlugin executorPlugin() {
        return new ExecutorPlugin() {
            @Override public void init(PluginContext context, Map<String, String> configuration) {
                install();
            }
        };
    }
    private static final ExactDialect POSTGRES = new ExactDialect(PostgresDialect$.MODULE$, "\"");
    private static final ExactDialect MYSQL = new ExactDialect(MySQLDialect$.MODULE$, "`");
    public static synchronized void install() {
        // AggregatedDialect delegates types/quoting, but not expression or query
        // builders. Replace the matching native dialect in this query JVM only.
        JdbcDialects.unregisterDialect(PostgresDialect$.MODULE$);
        JdbcDialects.unregisterDialect(MySQLDialect$.MODULE$);
        JdbcDialects.registerDialect(POSTGRES);
        JdbcDialects.registerDialect(MYSQL);
    }
    /** Startup parameter, also used by the Python metadata guard. Native
     * functions/operators must not resolve through the source role's path.
     */
    public static String withBuiltinSearchPath(String url) {
        if (url == null || !url.startsWith("jdbc:postgresql:")) {
            throw new IllegalArgumentException("query catalog source invalid");
        }
        return url + (url.contains("?") ? "&" : "?") + "currentSchema=pg_catalog";
    }

    /** Native JDBC scan builder and type mapping, with explicit PG ONLY scope.
     * Never resolve metadata via super.loadTable: that would first inspect a
     * potentially expanded inheritance tree before applying the constraint.
     */
    public static final class OnlyPostgresCatalog extends JDBCTableCatalog {
        private Map<String, String> connection;
        public OnlyPostgresCatalog() {}
        @Override public void initialize(String name, CaseInsensitiveStringMap options) {
            connection = new HashMap<>(options.asCaseSensitiveMap());
            if (!options.get("url").startsWith("jdbc:postgresql:")) {
                throw new IllegalArgumentException("query catalog source invalid");
            }
            super.initialize(name, options);
        }
        @Override public Table loadTable(Identifier identifier) {
            if (identifier.namespace().length != 1) {
                throw new IllegalArgumentException("query catalog identifier invalid");
            }
            String table = quote(identifier.namespace()[0]) + "." + quote(identifier.name());
            Map<String, String> values = new HashMap<>(connection);
            values.put("url", withBuiltinSearchPath(connection.get("url")));
            values.put("dbtable", "(SELECT * FROM ONLY " + table + ") AS addp_exact_source");
            scala.collection.immutable.Map<String, String> parameters =
                JavaConverters.mapAsScalaMapConverter(values).asScala()
                    .toMap(scala.Predef.<Tuple2<String, String>>$conforms());
            JDBCOptions options = new JDBCOptions(parameters);
            return new JDBCTable(identifier, JDBCRDD.resolveTable(options), options);
        }
        private static String quote(String name) {
            return "\"" + name.replace("\"", "\"\"") + "\"";
        }
    }

    private static final class ExactDialect extends JdbcDialect {
        private final JdbcDialect nativeDialect;
        private final String quote;
        ExactDialect(JdbcDialect nativeDialect, String quote) {
            this.nativeDialect = nativeDialect;
            this.quote = quote;
        }
        @Override public boolean canHandle(String url) { return nativeDialect.canHandle(url); }
        @Override public String quoteIdentifier(String name) {
            return quote + name.replace(quote, quote + quote) + quote;
        }
        @Override public Option<DataType> getCatalystType(int type, String name, int size, MetadataBuilder metadata) {
            return nativeDialect.getCatalystType(type, name, size, metadata);
        }
        @Override public Option<JdbcType> getJDBCType(DataType type) { return nativeDialect.getJDBCType(type); }
        @Override public boolean isSupportedFunction(String name) { return nativeDialect.isSupportedFunction(name); }
        @Override public Option<String> compileExpression(Expression expression) {
            if (nativeDialect != MySQLDialect$.MODULE$) {
                return super.compileExpression(expression);
            }
            // Preserve native MySQL null-order and aggregate compilation; its
            // builder's only changed method quotes the original field segments.
            MySQLDialect.MySQLSQLBuilder builder = new MySQLDialect.MySQLSQLBuilder() {
                @Override public String visitNamedReference(NamedReference reference) {
                    return java.util.Arrays.stream(reference.fieldNames())
                        .map(ExactDialect.this::quoteIdentifier).collect(java.util.stream.Collectors.joining("."));
                }
            };
            try {
                return Option.apply(builder.build(expression));
            } catch (RuntimeException unsupported) {
                // Same unsupported-expression contract as the native dialect.
                return Option.empty();
            }
        }
        @Override public JdbcSQLQueryBuilder getJdbcSQLQueryBuilder(JDBCOptions options) {
            return nativeDialect == MySQLDialect$.MODULE$
                ? new MySQLDialect.MySQLSQLQueryBuilder(this, options)
                : new JdbcSQLQueryBuilder(this, options);
        }
        @Override public boolean supportsLimit() { return nativeDialect.supportsLimit(); }
        @Override public boolean supportsOffset() { return nativeDialect.supportsOffset(); }
        @Override public boolean supportsTableSample() { return nativeDialect.supportsTableSample(); }
        @Override public String getTableSample(org.apache.spark.sql.execution.datasources.v2.TableSampleInfo sample) {
            return nativeDialect.getTableSample(sample);
        }
        @Override public java.sql.Timestamp convertJavaTimestampToTimestamp(java.sql.Timestamp value) {
            return nativeDialect.convertJavaTimestampToTimestamp(value);
        }
        @Override public java.time.LocalDateTime convertJavaTimestampToTimestampNTZ(java.sql.Timestamp value) {
            return nativeDialect.convertJavaTimestampToTimestampNTZ(value);
        }
        @Override public java.sql.Timestamp convertTimestampNTZToJavaTimestamp(java.time.LocalDateTime value) {
            return nativeDialect.convertTimestampNTZToJavaTimestamp(value);
        }
        @Override public void beforeFetch(java.sql.Connection connection, scala.collection.immutable.Map<String, String> properties) {
            nativeDialect.beforeFetch(connection, properties);
        }
        @Override public String getTableExistsQuery(String table) { return nativeDialect.getTableExistsQuery(table); }
        @Override public String getSchemaQuery(String table) { return nativeDialect.getSchemaQuery(table); }
        @Override public String[][] listSchemas(java.sql.Connection connection, JDBCOptions options) {
            return nativeDialect.listSchemas(connection, options);
        }
        @Override public boolean schemasExists(java.sql.Connection connection, JDBCOptions options, String schema) {
            return nativeDialect.schemasExists(connection, options, schema);
        }
    }
}
