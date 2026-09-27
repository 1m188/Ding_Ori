/*
    一些全局状态：功能表（每个功能的名称、热键、命令、开关）与程序状态（游戏 pid、注入进度）等。

    ⚠ 这里**只有状态**，没有任何业务逻辑行为。

    为什么功能表是数组 + 线性查找，而不是"按键 → 功能"的字典：
      1. 界面要按顺序列出全部功能，HOME 全开/全关也要遍历全部功能 —— 有序集合
         无论如何都得有，字典只会变成第二份需要同步的映射。
      2. 字典的 key 不能是单个虚拟键：小键盘 1 与 Ctrl+小键盘 1 的 VK 都是 0x61，
         却必须指向不同功能，所以 key 只能是由 (编号, 是否按 Ctrl) 拼出来的复合值，
         比直接比两个字段更绕。
      3. 功能数量有限，查一次按键比较次数有限，字典那点 O(1) 收益为零，却多出
         "两份表必须一致"这个静默失效点（漏登记一项 = 热键没反应，不报错）。
      所以热键作为功能自己的属性存在 Feature 上，查找就是扫描比对。
*/

namespace OriTrainer
{
    // 与游戏内载荷的连接状态。由 PipeClient 的后台线程写，界面读来显示。
    internal enum ConnectionState
    {
        NoGame,    // 没有游戏进程（还没启动 / 已退出）
        Waiting,   // 有游戏进程，但管道还没连上（正在重试，或已被别的实例占用）
        Injecting, // 正在注入载荷
        Connected, // 管道已连通，命令可以发送
    }

    internal sealed class Feature
    {
        // 命令名。必须与 DLL 侧 OriTrainerDLL.Features 下的类名逐字一致 ——
        // Loader.Dispatch 是按这个名字反射找类型的，写错了不会报错，只会静默无响应。
        public readonly string Name;

        // 界面显示名。与 Name 分开是因为前者是协议标识（英文类名），
        // 后者要给人看（中文）；合成一个必然牺牲其中一边。
        public readonly string Label;

        public readonly int Digit;     // 小键盘编号 1..9
        public readonly bool NeedCtrl; // 是否要按住 Ctrl

        // 是否开启。⚠ 这只是"本程序最后发出的命令是 Start"，
        // 不是"功能确实在游戏里生效了" —— DLL 不回话，无从确认。
        public bool On;

        public Feature(string name, string label, int digit, bool needCtrl = false)
        {
            Name = name;
            Label = label;
            Digit = digit;
            NeedCtrl = needCtrl;
        }

        public string StartCommand { get { return Name + " Start"; } }
        public string StopCommand { get { return Name + " Stop"; } }

        public string HotkeyLabel { get { return (NeedCtrl ? "Ctrl + 小键盘 " : "小键盘 ") + Digit; } }
    }

    internal static class Status
    {
        // 游戏进程 ID；0 表示还没找到游戏进程。
        public static int Pid;

        // 与游戏内载荷的连接状态。⚠ 只有 PipeClient 的后台线程写这里。
        public static ConnectionState Connection;

        // 顺序即界面顺序：先小键盘（普通功能），再 Ctrl+小键盘（特殊功能）。
        public static readonly Feature[] Features =
        {
            // ===== 普通功能：小键盘 =====
            new Feature("UnlimitedLife",      "无限生命",               1),
            new Feature("UnlimitedEnergy",    "无限能量",               2),
            new Feature("SoulFlameNoCooldown","灵魂链接无需冷却",        3),
            new Feature("SoulFlameAnywhere",  "不安全区域建灵魂链接",    4),
            new Feature("SuperJump",          "超级跳",                 5),
            new Feature("InfiniteDoubleJump", "无限二段跳",             6),
            new Feature("InfiniteSkillPoints","无限能力点数",            7),
            new Feature("ShowMap",            "显示地图",               8),
#if DE
            new Feature("InfiniteDash",       "无限冲刺",               9),
#endif

            // ===== 特殊功能：Ctrl+小键盘 =====
            new Feature("ZeroDeaths",         "死亡数归零",         1, true),
            new Feature("CompleteExploration","100% 探索",          2, true),
            new Feature("UnlockAllAbilities", "解锁全部基础技能",    3, true),
            new Feature("ResetTime",          "重置时间",           4, true),
            new Feature("GrantKeys",          "获得三把钥匙",        5, true),
        };
    }
}
