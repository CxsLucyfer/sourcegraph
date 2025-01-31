package com.sourcegraph.config;

import com.intellij.openapi.project.Project;
import com.sourcegraph.find.Search;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

public interface SourcegraphService {
  @NotNull
  static SourcegraphProjectService getInstance(@NotNull Project project) {
    return project.getService(SourcegraphProjectService.class);
  }

  @Nullable
  String getInstanceType();

  @Nullable
  String getSourcegraphUrl();

  @Nullable
  String getAccessToken();

  @Nullable
  String getCustomRequestHeaders();

  @Nullable
  String getDefaultBranchName();

  @Nullable
  String getRemoteUrlReplacements();

  @Nullable
  Search getLastSearch();

  String getEnterpriseAccessToken();

  boolean areChatPredictionsEnabled();

  String getCodebase();
}
