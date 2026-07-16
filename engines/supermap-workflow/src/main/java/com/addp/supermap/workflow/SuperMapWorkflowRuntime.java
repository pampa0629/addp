package com.addp.supermap.workflow;

import com.fasterxml.jackson.databind.ObjectMapper;

public final class SuperMapWorkflowRuntime {
  static final ObjectMapper MAPPER = new ObjectMapper();
  static final String ENGINE_TYPE = "supermap_workflow";
  static final String SPS_FACTORY = "addp_supermap_workflow";

  private SuperMapWorkflowRuntime() {}

  public static void main(String[] args) throws Exception {
    SuperMapHttpServer.start();
  }
}
