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

/** Read-only query driver extension for Spark 3.5.0 identifier escaping.
 * Native AggregatedDialect retains the built-in JDBC type mappings and uses
 * this first dialect's quoteIdentifier. The query-only PostgreSQL catalog bounds
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
    public static void install() {
        JdbcDialects.registerDialect(new ExactDialect("jdbc:postgresql:", "\""));
        JdbcDialects.registerDialect(new ExactDialect("jdbc:mysql:", "`"));
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
        private final String prefix;
        private final String quote;
        ExactDialect(String prefix, String quote) { this.prefix = prefix; this.quote = quote; }
        @Override public boolean canHandle(String url) { return url.startsWith(prefix); }
        @Override public String quoteIdentifier(String name) {
            return quote + name.replace(quote, quote + quote) + quote;
        }
    }
}
