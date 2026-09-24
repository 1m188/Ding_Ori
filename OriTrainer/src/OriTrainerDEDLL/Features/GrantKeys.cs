using System;
using System.Threading;

namespace OriTrainerDEDLL.Features
{
    // 获得三把钥匙：把 Keys.GinsoTree / ForlornRuins / MountHoru 三个静态标记保持为 true。
    //
    // ---- 为什么这里什么都不用做 ----
    // 我们注入在 Mono 内部，Sein.World.Keys 就是一个普通的 static class
    // （abstract sealed，三个 public static bool，**没有静态构造函数**，
    //   TypeInitializer 为空），直接赋值即可。没有内存扫描、没有偏移、没有反射。
    //
    // ---- 为什么直接赋值就等价于"捡到钥匙"----
    // 全程序集 IL 扫描显示，写这三个字段的只有三处：
    //     SetSeinWorldStateAction.Perform   stsfld Keys.X = IsTrue   ← 真实拾取路径
    //     SeinWorldState.OnGameReset        stsfld Keys.X = false
    //     DebugMenuB.XxxKeySetter           stsfld Keys.X = value    ← 官方调试菜单
    // 而读它的只有 SeinWorldStateCondition.Validate（开门判定，IL 里的 switch 分支
    // 是 GinsoTreeKey=3 → GinsoTree、ForlornRuinsKey=10 → ForlornRuins、
    // MountHoruKey=11 → MountHoru）。也就是说本功能做的事和游戏自己捡到钥匙做的
    // 事逐字节相同，**不跳过副本内容**，只是省了跑过去捡这一步。
    //
    // ---- 为什么必须持续写入 ----
    // 这三个 bool 本身毫无自我保护能力：真正的"永久"来自存档，不是来自字段。
    // 它们被 SeinWorldState.Serialize 用 ldsflda 取址序列化（即存档字段），而
    // 检查点建立时 GameController.CreateCheckpoint → SaveSceneManager.SaveWithoutClearing
    // 会把当时的 bool 写进检查点；死亡时 RestoreCheckpointController.RestoreCheckpoint
    // → SaveGameController.RestoreCheckpointPart1 再读回来。
    //
    // ⚠ 那个传进 SaveSceneManager.Load 的 HashSet 是**白名单**而非排除集（纯看
    //   反编译出的 C# 极易误判），IL 是 HashSet.Contains + brfalse 跳过 Serialize，
    //   而 RestoreCheckpointPart1 恰恰把 SeinWorldState.Instance 加进了这个集合。
    //   所以死亡恢复检查点**会把钥匙打回**成检查点里的值。
    //
    // 因此一次写入有真实缺口：置位 → 在游戏建下一个检查点之前死了 → 恢复读到旧
    // 检查点的 false → 钥匙没了。持续写把这个窗口彻底关掉。另外 GameController.
    // RestartGame / RestartOneLifeMode 会触发 GameScheduler.OnGameReset 把三个字段
    // 全部清 0，同样需要补回。
    // 写了守卫是因为正常游玩时它们几乎恒为 true，写入次数 ≈ 被回滚的次数，而不是
    // 每秒 100 次（与 ZeroDeaths 同理）。
    //
    // ---- 为什么不需要主线程钩子 ----
    // 与 UnlockAllAbilities / InfiniteDash 不同，那些必须挂 OnGameFixedUpdate 是因为
    // 要调 Object.Instantiate 之类的 Unity API。这里只是三个静态 bool，且消费者
    // ActivateBasedOnCondition / ActivationBasedOnCondition 每 FixedUpdate 轮询
    // Condition.Validate()，标志一置，门和暂停界面图标会在下一个 FixedUpdate（20ms 内）
    // 自动跟上。写静态字段本身也不挑线程，因此与 ZeroDeaths / CompleteExploration
    // 一样用定时器即可。
    // 也因为没有实例（static class），不像 ZeroDeaths 那样需要判 Instance == null。
    //
    // ---- 停止不还原 ----
    // 与 UnlockAllAbilities 同理：SeinWorldState.Serialize 会持久化这三个字段，
    // 开启期间游戏只要存过一次档（过检查点），它们就成了存档里的既有状态，还原也
    // 收不回来。刻意不还原，避免给出"能收回来"的假象。
    //
    // ---- 已知连带效果 ----
    //   · 暂停界面（InventoryManager）上的三个钥匙图标是 GameObject，全程序集里
    //     **零 IL 引用**（对照：同类其他字段有 45 处引用），说明它们由预制体上的
    //     Condition 驱动，预期会跟着标志自动显示。
    //   · Events.get_Progression() 只被 SaveSlotInfo.FillData 读取（存档槽 UI 的
    //     进度标签），本功能不碰 Sein.World.Events，与元素系统无关。
    //   · 游戏另有一套 Keystones（楔石，SeinInventory.Keystones 是 Int32，
    //     DoorWithSlots 消耗它）——那是"量"，与本功能的"三把门钥匙"是两套无关系统。
    public static class GrantKeys
    {
        private const int IntervalMs = 10;

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                if (!Sein.World.Keys.GinsoTree) Sein.World.Keys.GinsoTree = true;         // Ginso Tree 门钥匙
                if (!Sein.World.Keys.ForlornRuins) Sein.World.Keys.ForlornRuins = true;   // Forlorn Ruins 门钥匙
                if (!Sein.World.Keys.MountHoru) Sein.World.Keys.MountHoru = true;         // Mount Horu 门钥匙
            }
            catch { }
        }
    }
}
