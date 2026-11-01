package com.addp.spark.query;

import java.util.Collections;
import java.util.Map;
import org.apache.spark.SparkContext;
import org.apache.spark.api.plugin.DriverPlugin;
import org.apache.spark.api.plugin.ExecutorPlugin;
import org.apache.spark.api.plugin.PluginContext;
import org.apache.spark.api.plugin.SparkPlugin;
import org.apache.spark.sql.jdbc.JdbcDialect;
import org.apache.spark.sql.jdbc.JdbcDialects;

/** Read-only query driver extension for Spark 3.5.0 identifier escaping.
 * Native AggregatedDialect retains the built-in JDBC type mappings and uses
 * this first dialect's quoteIdentifier. No catalog, session or write route is added.
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
