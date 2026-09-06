using System;
using System.Collections.Generic;
using OriTrainer.Core;

namespace OriTrainer.Ori
{
    /// <summary>
    /// 原版功能清单与热键分配（风灵月影风格：数字键切换，HOME 全关）。
    /// 功能 1-3 为 AOB 代码补丁（对应 CE 表的 aobscan 脚本，版本稳健）；
    /// 功能 4-6 为指针链冻结（对应 CE 表的 pointer 条目）。
    /// </summary>
    internal static class OriFeatures
    {
        public static CheatFeature[] BuildAll()
        {
            var list = new List<CheatFeature>();

            // 来源: 3943 Dix Dark "Inf. health"（原脚本把伤害结算改 NOP、死亡分支改 JMP）
            list.Add(new AobPatchFeature("无限生命", 1,
                new AobPatchFeature.PatchSpec
                {
                    Description = "跳过伤害扣血(health)",
                    Pattern = P("D9 04 24 83 C4 04 DE E9 D9 5E 1C D9 EE D9 46 1C 83 EC 08 83 EC 04 D9 1C 24 83 EC 04 D9 1C 24 E8 ?? ?? ?? ?? 83 C4 10 D9 5E 1C D9 46 1C D9 5E 24 8D 65 FC 5E C9 C3"),
                    PatchOffset = 0,
                    Replacement = B("90 90 90 83 C4 04 90 90")
                },
                new AobPatchFeature.PatchSpec
                {
                    Description = "跳过死亡分支(dying)",
                    Pattern = P("7A 31 77 2F 8B 46 20 8B 40 40 8B 40 0C 83 EC 08 FF B5 28 FF FF FF 50 39 00 E8 ?? ?? ?? ?? 83 C4 10 83 EC 08 57 56 E8 ?? ?? ?? ?? 83 C4 10 E9 ?? ?? ?? ?? 8B 46 20"),
                    PatchOffset = 0,
                    Replacement = B("EB")
                }));

            // 来源: 3943 Dix Dark "Inf. energy" + 3834 ubiByte "Infinite Energy"（两个版本的扣能点，哪个匹配补哪个）
            list.Add(new AobPatchFeature("无限能量", 2,
                new AobPatchFeature.PatchSpec
                {
                    Description = "能量扣除点A(DixDark)",
                    Pattern = P("D9 45 0C DE E9 D9 5F 20 D9 47 20 D9 EE DF F1 DD D8 76 05 D9 EE D9 5F 20 D9 47 20 D9 5F 18 8D 65 FC 5F C9 C3"),
                    PatchOffset = 0,
                    Replacement = B("90 90 90 90 90")
                },
                new AobPatchFeature.PatchSpec
                {
                    Description = "能量扣除点B(ubiByte)",
                    Pattern = P("D9 5F 20 D9 47 20 D9 EE DF F1 DD D8 ?? ?? ?? ?? D9 5F 20 D9 47 20 D9 5F 18 8D 65 FC"),
                    PatchOffset = 0,
                    Replacement = B("90 90 90")
                }));

            // 来源: 3834 ubiByte "Infinite Skill Points"（跳过技能点扣减的存储指令）
            list.Add(new AobPatchFeature("无限技能点", 3,
                new AobPatchFeature.PatchSpec
                {
                    Description = "技能点扣减存储",
                    Pattern = P("89 48 24 8B 47 58 83 EC 0C 50"),
                    PatchOffset = 0,
                    Replacement = B("90 90 90")
                }));

            // 指针冻结类（CE 表 pointer 条目转写；激活瞬间捕获当前值并保持）
            list.Add(new FreezeFeature("死亡数冻结", 4, "ori.exe",
                OriOffsets.Normalize(OriOffsets.TotalDeathsChainCe), false));
            list.Add(new FreezeFeature("技能点数冻结", 5, "mono.dll",
                OriOffsets.Normalize(OriOffsets.TotalSkillPointsChainCe), false));
            list.Add(new FreezeFeature("精神点(经验)冻结", 6, "mono.dll",
                OriOffsets.Normalize(OriOffsets.TotalSpiritPointsChainCe), false));

            return list.ToArray();
        }

        /// <summary>解析 "AA BB ?? CC" 形式的特征码，?? 为通配（-1）。</summary>
        private static int[] P(string spec)
        {
            var parts = spec.Split(' ');
            var r = new int[parts.Length];
            for (int i = 0; i < parts.Length; i++)
                r[i] = parts[i] == "??" ? -1 : Convert.ToInt32(parts[i], 16);
            return r;
        }

        private static byte[] B(string spec)
        {
            var parts = spec.Split(new[] { ' ' }, StringSplitOptions.RemoveEmptyEntries);
            var r = new byte[parts.Length];
            for (int i = 0; i < parts.Length; i++)
                r[i] = Convert.ToByte(parts[i], 16);
            return r;
        }
    }
}
