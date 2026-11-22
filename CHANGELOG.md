# Changelog

## 1.0.0 (2026-11-22)


### ⚠ BREAKING CHANGES

* **cloud:** yet_cloud_rack and the racks API take no kind.
* **settings:** /api/settings/aws lists accounts; one is added with a POST and connected with a PATCH of /aws/{id}. The SDKs' AWS is a list (List, Add, Connect, Update, Check, Remove); Terraform's yet_settings_connection_aws is yet_settings_aws_account and yet_settings_aws_account_role, with yet_settings_aws_launch.
* **settings:** the AWS connection takes role_arn and region, not an access key; yet_settings_connection_aws likewise, with the new yet_settings_connection_aws_launch for its stack; a database's backup routine no longer takes a company bucket.
* **cloud:** /v1/applications/{id}/resources is now .../addons, an attachment takes database_id, not resource_id, and backups and queries say database_id. The SDKs' methods are Addons, CreateAddon and DeleteAddon (addons, create_addon, delete_addon), and Terraform's yet_cloud_application_resource is yet_cloud_application_addon.
* **settings:** with an API key, giving someone the company's Settings (runs_settings true; Terraform's admin = true) and setting someone else's password again are refused. yet_settings_person's admin is read or set to false only, and its password is the one they were added with.

### Features

* **cloud:** a bucketdb's history: explore, restore, download at a cost ([40a970e](https://github.com/yetcombr/yet/commit/40a970ed3cdbaf738da0475dd8a7cb0f7b3f74c0))
* **cloud:** a function moves region without going down ([4e677e0](https://github.com/yetcombr/yet/commit/4e677e0201c4484f51393232c4ec8e20562e69ec))
* **cloud:** a function of an application yet builds ([f1571ff](https://github.com/yetcombr/yet/commit/f1571ff686ae4d674dbe6eca63cb82d1da1eb166))
* **cloud:** a function's timeout, and the role it runs as ([81be20e](https://github.com/yetcombr/yet/commit/81be20ea81400ce6846a920644b521223358d638))
* **cloud:** a k3s runtime without an ingress, for runners only ([2f4da3b](https://github.com/yetcombr/yet/commit/2f4da3b89deda820e25ff0133c839e79e703734e))
* **cloud:** a lambda runtime in each region, one serverless server an account ([99ba4fa](https://github.com/yetcombr/yet/commit/99ba4fa0e4fcebaeeceeb91a7ebb92e3db456138))
* **cloud:** a runtime's name is its server's, not the company's ([9110c1e](https://github.com/yetcombr/yet/commit/9110c1eaca700e17bf6373b397d12ec9abbc6778))
* **cloud:** add a database in steps, a bucketdb's bucket chosen ([b467114](https://github.com/yetcombr/yet/commit/b46711461f687f3452abd9f8d69da71545f998ee))
* **cloud:** an application's resources are its add-ons ([f827953](https://github.com/yetcombr/yet/commit/f827953e1a35ba040f52be30bfe475b62e13d100))
* **cloud:** applications' buckets under a prefix of the company's ([2a2fce8](https://github.com/yetcombr/yet/commit/2a2fce863a47087540f9e062fbe8b0add8ec664b))
* **cloud:** bucketdb as an environment's database too ([c96c5ae](https://github.com/yetcombr/yet/commit/c96c5aec4e49ecb7983df138684d211e188b1d48))
* **cloud:** bucketdb, a postgres in a bucket beside its application ([474f2e7](https://github.com/yetcombr/yet/commit/474f2e7433d8b37fbdbf05a7e519234330a711f0))
* **cloud:** build a monorepo's folder; an application's address inside ([d414d3b](https://github.com/yetcombr/yet/commit/d414d3b69503b25525e5725dc8314dd769710f5e))
* **cloud:** choose each stage's bucket, with AWS's prices first ([b0f9155](https://github.com/yetcombr/yet/commit/b0f91557ea628dd33e73c283f53abc83bb73f683))
* **cloud:** connect several GitHub accounts ([0e9aca5](https://github.com/yetcombr/yet/commit/0e9aca5a41be8e9ee6c558df14edbb4ad9f2f238))
* **cloud:** functions built in the account, on codebuild ([3a2aa35](https://github.com/yetcombr/yet/commit/3a2aa352ec11b11e35f6aef75383d5d932de1c43))
* **cloud:** functions watched, with a policy, at the root; signed schedules ([c2aa2a1](https://github.com/yetcombr/yet/commit/c2aa2a11574478914264f2088a648f39dbd96de5))
* **cloud:** GitHub runners on k3s, sandboxed, growing with their jobs ([68300fd](https://github.com/yetcombr/yet/commit/68300fd708b48b3e10d683b1c9ebb614deee7f0c))
* **cloud:** GitHub runners on Windows's k3s too, gVisor in its WSL ([055a2c9](https://github.com/yetcombr/yet/commit/055a2c98b585abac964771c2ed8777d76a817c7d))
* **cloud:** keep or take an application's add-ons, adopt the kept ([cecb1b1](https://github.com/yetcombr/yet/commit/cecb1b13f08d9b773fc9cbf0539774c06892eb26))
* **cloud:** lambda in several regions, each stage choosing, with flags ([56cb8c6](https://github.com/yetcombr/yet/commit/56cb8c6a7cffbe9fcbd8e10ed66ecfe6debf2783))
* **cloud:** one AWS identity per application stage ([6e456a6](https://github.com/yetcombr/yet/commit/6e456a66d61e425f4aa6c3fe88d83a4deb451911))
* **cloud:** racks of no kind; servers started in AWS ([346dd08](https://github.com/yetcombr/yet/commit/346dd08a6af967b99cb101c1de8f4bd35d186cc1))
* **cloud:** rename an environment; its id at hand ([d2d563d](https://github.com/yetcombr/yet/commit/d2d563dff9815232f0b0fcc1f43ecd0800575fa9))
* **cloud:** restore, explore and upload database copies ([ac7f46e](https://github.com/yetcombr/yet/commit/ac7f46e7b29231b32baa249405519707b00cf147))
* **cloud:** run an application's stage as a lambda function ([9caac43](https://github.com/yetcombr/yet/commit/9caac43991f53dd2df22f40836ebd8da7ab89bf5))
* **cloud:** S3 buckets as application add-ons ([23b63d8](https://github.com/yetcombr/yet/commit/23b63d87fc87746ec7f8b1355d026aadae818dc8))
* **cloud:** schedules, on eventbridge for functions, cronjobs on runtimes ([e3fdf9d](https://github.com/yetcombr/yet/commit/e3fdf9d8899f77dc73a8b16e2ba3b7d08289d533))
* **cloud:** serverless servers, lambda runtimes on them, environments on those ([3b95730](https://github.com/yetcombr/yet/commit/3b95730dbd0c056024d9044e561e038bb2b20d2f))
* **cloud:** update a server's agent now; deploy after its image ([53f5045](https://github.com/yetcombr/yet/commit/53f50451596f075defc2303a90172b126f8cb4a4))
* **cloud:** usage, each aws account's quotas, on connection v6 ([cf04e3e](https://github.com/yetcombr/yet/commit/cf04e3ec0e0b11005a96ab16850e4c4569577b08))
* **cloud:** yap in the SDKs, Terraform and the portal ([cee4825](https://github.com/yetcombr/yet/commit/cee4825f755b4d25aeba655556c19e9c6aadc2a0))
* **mail:** shared mailboxes, and a copy of what the API sends ([f867aa0](https://github.com/yetcombr/yet/commit/f867aa0ea076ebc35b61b06e35cfe9c23ad6ca89))
* **provider:** Terraform provider on the Go SDK ([616e6e8](https://github.com/yetcombr/yet/commit/616e6e8b4154ee8dde78bc75ba0ea908629d5c4e))
* **sdk:** every product at yet's API, by its prefix ([824bbba](https://github.com/yetcombr/yet/commit/824bbba7e1dbfb94854568212019825f963afc1f))
* **settings:** a bucket of each product's, made by the AWS stack ([e4fdf66](https://github.com/yetcombr/yet/commit/e4fdf66b42662f42f7ebb278e1a0e6f402a14fb2))
* **settings:** connect AWS by a role Yet's stack makes, not a key ([953ddd8](https://github.com/yetcombr/yet/commit/953ddd87dff268e8eaf4f43cf9735e230c9c8157))
* **settings:** several AWS accounts, each for what it is used for ([e416e47](https://github.com/yetcombr/yet/commit/e416e4746e194f3529b2f067610bd57a87b0d292))
* **settings:** sign in with yet for a company's own tools ([c03768c](https://github.com/yetcombr/yet/commit/c03768c5b736de8efc9c69b291beaf5ec5602a5b))
* **settings:** sign in with yet over saml too ([38fff57](https://github.com/yetcombr/yet/commit/38fff57320dee62bd6ce5676bd4afe65c8f08f65))
* **settings:** subdomains and an API to send email from them ([6dea575](https://github.com/yetcombr/yet/commit/6dea575ea4e4b261d8060b2f382b95321a1365b4))
* **sso:** sign in as Google's does — Yet on top, the tool and its terms ([824c68d](https://github.com/yetcombr/yet/commit/824c68d59858b9aafe1639e080875dd61e883af2))
* **terraform:** a namespace data source, by name or id ([f21f6b2](https://github.com/yetcombr/yet/commit/f21f6b252e402d59f9815d44b152d8e929fd71a6))
* **terraform:** data sources found by id too ([8076283](https://github.com/yetcombr/yet/commit/807628325eb25a2273f18ad991f71fa6c18db270))
* **terraform:** ports, checks, scaling, pod delete and branches ([7a9f5f2](https://github.com/yetcombr/yet/commit/7a9f5f2d532f6f1c9fd3df055a07c4c36b4aff50))
* **terraform:** sizes per environment ([b665004](https://github.com/yetcombr/yet/commit/b66500411eebfb0353e0960601645945606b570c))
* **vault:** secrets in the company's account, read by its applications ([840718a](https://github.com/yetcombr/yet/commit/840718a123427bb27f7f5df827218f84e17cf0a3))


### Bug Fixes

* **cloud:** a stage's bucket only in the account chosen for it ([99c00dd](https://github.com/yetcombr/yet/commit/99c00dd6990197b8f7b0d4826ca55b23ae3f807f))
* **cloud:** keep each company to its own names on the edge ([08d04e0](https://github.com/yetcombr/yet/commit/08d04e052399a7de0e5adc7a3c3dd05918cd4a53))
* **settings:** fence passwords, roles and yet's admins from a company ([b6206ae](https://github.com/yetcombr/yet/commit/b6206aec04bce69ba85c657d786d2bb80213ab38))
* **terraform:** the environment data source reads its functions ([34bddf2](https://github.com/yetcombr/yet/commit/34bddf26e5d3762e0060c1b738e65b926ee4abbc))
