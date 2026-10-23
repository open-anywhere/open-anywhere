<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/api/13619-agentapi-openapi | 由 sync-docs.py 自动生成，请勿手改 -->

# OpenAPI 规范



最近更新时间: 2026-10-14 14:03:34

完整请求、响应字段和数据结构见 [下载 OpenAPI YAML](https://dn-odum9helk.qbox.me/FvPJkqgApx2DcsC4EdR9Ym2mYv_t)。该文件可导入支持 OpenAPI 的接口工具。

## 接口索引

| 方法     | 路径                                                                               | 说明                                  |
| ------ | -------------------------------------------------------------------------------- | ----------------------------------- |
| GET    | `/v1/agents`                                                                     | List agents                         |
| POST   | `/v1/agents`                                                                     | Create agent                        |
| DELETE | `/v1/agents/{id}`                                                                | Delete agent                        |
| GET    | `/v1/agents/{id}`                                                                | Get agent                           |
| PATCH  | `/v1/agents/{id}`                                                                | Patch agent                         |
| POST   | `/v1/agents/{id}`                                                                | Update agent                        |
| POST   | `/v1/agents/{id}/archive`                                                        | Archive agent                       |
| GET    | `/v1/agents/{id}/versions`                                                       | List agent versions                 |
| GET    | `/v1/deployment_runs`                                                            | List deployment runs                |
| GET    | `/v1/deployment_runs/{deployment_run_id}`                                        | Get deployment run                  |
| GET    | `/v1/deployments`                                                                | List deployments                    |
| POST   | `/v1/deployments`                                                                | Create deployment                   |
| GET    | `/v1/deployments/{deployment_id}`                                                | Get deployment                      |
| POST   | `/v1/deployments/{deployment_id}`                                                | Update deployment                   |
| POST   | `/v1/deployments/{deployment_id}/archive`                                        | Archive deployment                  |
| POST   | `/v1/deployments/{deployment_id}/pause`                                          | Pause deployment                    |
| POST   | `/v1/deployments/{deployment_id}/run`                                            | Run deployment now                  |
| POST   | `/v1/deployments/{deployment_id}/unpause`                                        | Unpause deployment                  |
| GET    | `/v1/environments`                                                               | List environments                   |
| POST   | `/v1/environments`                                                               | Create environment                  |
| GET    | `/v1/environments/{environment_id}/work`                                         | List self-hosted work               |
| GET    | `/v1/environments/{environment_id}/work/poll`                                    | Poll self-hosted environment work   |
| GET    | `/v1/environments/{environment_id}/work/stats`                                   | Get self-hosted work queue stats    |
| GET    | `/v1/environments/{environment_id}/work/{work_id}`                               | Get self-hosted work                |
| POST   | `/v1/environments/{environment_id}/work/{work_id}`                               | Update self-hosted work metadata    |
| POST   | `/v1/environments/{environment_id}/work/{work_id}/ack`                           | Acknowledge self-hosted work        |
| POST   | `/v1/environments/{environment_id}/work/{work_id}/heartbeat`                     | Heartbeat self-hosted work          |
| POST   | `/v1/environments/{environment_id}/work/{work_id}/stop`                          | Stop self-hosted work               |
| DELETE | `/v1/environments/{id}`                                                          | Delete environment                  |
| GET    | `/v1/environments/{id}`                                                          | Get environment                     |
| POST   | `/v1/environments/{id}`                                                          | Update environment                  |
| POST   | `/v1/environments/{id}/archive`                                                  | Archive environment                 |
| GET    | `/v1/files`                                                                      | List files                          |
| POST   | `/v1/files`                                                                      | Upload file                         |
| DELETE | `/v1/files/{id}`                                                                 | Delete file                         |
| GET    | `/v1/files/{id}`                                                                 | Get file metadata                   |
| GET    | `/v1/files/{id}/content`                                                         | Download file content               |
| GET    | `/v1/memory_stores`                                                              | List memory stores                  |
| POST   | `/v1/memory_stores`                                                              | Create memory store                 |
| DELETE | `/v1/memory_stores/{memory_store_id}`                                            | Delete memory store                 |
| GET    | `/v1/memory_stores/{memory_store_id}`                                            | Get memory store                    |
| POST   | `/v1/memory_stores/{memory_store_id}`                                            | Update memory store                 |
| POST   | `/v1/memory_stores/{memory_store_id}/archive`                                    | Archive memory store                |
| GET    | `/v1/memory_stores/{memory_store_id}/memories`                                   | List memories                       |
| POST   | `/v1/memory_stores/{memory_store_id}/memories`                                   | Create memory                       |
| DELETE | `/v1/memory_stores/{memory_store_id}/memories/{memory_id}`                       | Delete memory                       |
| GET    | `/v1/memory_stores/{memory_store_id}/memories/{memory_id}`                       | Get memory                          |
| POST   | `/v1/memory_stores/{memory_store_id}/memories/{memory_id}`                       | Update memory                       |
| GET    | `/v1/memory_stores/{memory_store_id}/memory_versions`                            | List memory versions                |
| GET    | `/v1/memory_stores/{memory_store_id}/memory_versions/{memory_version_id}`        | Get memory version                  |
| POST   | `/v1/memory_stores/{memory_store_id}/memory_versions/{memory_version_id}/redact` | Redact memory version               |
| GET    | `/v1/sessions`                                                                   | List sessions                       |
| POST   | `/v1/sessions`                                                                   | Create session                      |
| DELETE | `/v1/sessions/{id}`                                                              | Delete session                      |
| GET    | `/v1/sessions/{id}`                                                              | Get session                         |
| POST   | `/v1/sessions/{id}`                                                              | Update session                      |
| POST   | `/v1/sessions/{id}/archive`                                                      | Archive session                     |
| GET    | `/v1/sessions/{id}/events`                                                       | List session events                 |
| POST   | `/v1/sessions/{id}/events`                                                       | Send session events                 |
| GET    | `/v1/sessions/{id}/events/stream`                                                | Stream session events               |
| GET    | `/v1/sessions/{id}/resources`                                                    | List session resources              |
| POST   | `/v1/sessions/{id}/resources`                                                    | Add session resource                |
| DELETE | `/v1/sessions/{id}/resources/{resource_id}`                                      | Delete session resource             |
| GET    | `/v1/sessions/{id}/resources/{resource_id}`                                      | Get session resource                |
| POST   | `/v1/sessions/{id}/resources/{resource_id}`                                      | Update session resource             |
| GET    | `/v1/skills`                                                                     | List skills                         |
| POST   | `/v1/skills`                                                                     | Create skill                        |
| DELETE | `/v1/skills/{skill_id}`                                                          | Delete skill                        |
| GET    | `/v1/skills/{skill_id}`                                                          | Get skill                           |
| GET    | `/v1/skills/{skill_id}/versions`                                                 | List skill versions                 |
| POST   | `/v1/skills/{skill_id}/versions`                                                 | Create skill version                |
| DELETE | `/v1/skills/{skill_id}/versions/{version}`                                       | Delete skill version                |
| GET    | `/v1/skills/{skill_id}/versions/{version}`                                       | Get skill version                   |
| GET    | `/v1/skills/{skill_id}/versions/{version}/content`                               | Download skill version content      |
| GET    | `/v1/skills/{skill_id}/versions/{version}/files`                                 | List skill version files            |
| GET    | `/v1/skills/{skill_id}/versions/{version}/files/content`                         | Download skill version file content |
| GET    | `/v1/vaults`                                                                     | List vaults                         |
| POST   | `/v1/vaults`                                                                     | Create vault                        |
| DELETE | `/v1/vaults/{vault_id}`                                                          | Delete vault                        |
| GET    | `/v1/vaults/{vault_id}`                                                          | Get vault                           |
| POST   | `/v1/vaults/{vault_id}`                                                          | Update vault                        |
| POST   | `/v1/vaults/{vault_id}/archive`                                                  | Archive vault                       |
| GET    | `/v1/vaults/{vault_id}/credentials`                                              | List credentials                    |
| POST   | `/v1/vaults/{vault_id}/credentials`                                              | Create credential                   |
| DELETE | `/v1/vaults/{vault_id}/credentials/{credential_id}`                              | Delete credential                   |
| GET    | `/v1/vaults/{vault_id}/credentials/{credential_id}`                              | Get credential                      |
| POST   | `/v1/vaults/{vault_id}/credentials/{credential_id}`                              | Update credential                   |
| POST   | `/v1/vaults/{vault_id}/credentials/{credential_id}/archive`                      | Archive credential                  |
