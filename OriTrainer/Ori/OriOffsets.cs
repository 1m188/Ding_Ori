namespace OriTrainer.Ori
{
    /// <summary>
    /// 原版《Ori and the Blind Forest》地址常量 —— 全程序唯一的地址维护点。
    /// 目标进程: ori.exe（Steam buildid 814852 / Build Number 5474 / Unity 5.0.0 / 32 位 Mono）
    ///
    /// 地址来源: fearlessrevolution.com 社区 CE 表（附件 3834 ubiByte、3943 Dix Dark），
    /// 全部以“模块基址 + 偏移链”或 AOB 特征码表达，不写死绝对地址。
    ///
    /// 实测说明:
    /// 1) 指针链数组保留 CE 表 XML 的原始书写顺序，Normalize() 在运行期转换成应用顺序。
    ///    若实机验证发现某条指针链不通，先尝试把 ReverseCeOffsets 改为 false（两种顺序互换）。
    /// 2) 所有功能默认 Verified=false（待验证），由使用者在游戏内确认。
    /// </summary>
    internal static class OriOffsets
    {
        public const string TargetProcessName = "ori";

        /// <summary>
        /// CE 解析约定开关。CE 表 XML 中 Offsets 的书写顺序是“XML 末条先应用、
        /// XML 首条加在最终地址上”；MemoryReader.WalkChain 则按“数组顺序依次应用、
        /// 首条加在模块基址上”。因此默认先反转再使用。实测不通时可改为 false 互换顺序。
        /// </summary>
        public const bool ReverseCeOffsets = true;

        // ---- 指针链（4 字节整数值，CE XML 原始顺序）----

        // CE 表(3834 ubiByte) "Total Deaths":  Address "ori.exe"+00A36164, Offsets: 14, D0, 750, 50, 2F8
        public static readonly int[] TotalDeathsChainCe = { 0x14, 0xD0, 0x750, 0x50, 0x2F8 };

        // CE 表(3834 ubiByte) "Total Skill Points": Address "mono.dll"+001F42C4, Offsets: 24, 38, 3C, 6DC, 50
        public static readonly int[] TotalSkillPointsChainCe = { 0x24, 0x38, 0x3C, 0x6DC, 0x50 };

        // CE 表(3834 ubiByte) "Total Spirit Points": Address "mono.dll"+001F42C4, Offsets: 2C, 38, 24, 67C, 50
        public static readonly int[] TotalSpiritPointsChainCe = { 0x2C, 0x38, 0x24, 0x67C, 0x50 };

        public static int[] Normalize(int[] ceOrderChain)
        {
            if (!ReverseCeOffsets) return (int[])ceOrderChain.Clone();
            var r = new int[ceOrderChain.Length];
            for (int i = 0; i < ceOrderChain.Length; i++)
                r[i] = ceOrderChain[ceOrderChain.Length - 1 - i];
            return r;
        }
    }
}
