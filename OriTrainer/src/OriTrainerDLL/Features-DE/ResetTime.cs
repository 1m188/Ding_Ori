using System;

namespace OriTrainerDLL.Features
{
    // 重置时间：把游玩计时器归零，暂停界面显示 0:00:00。
    //
    // ---- 这是全项目最简单的一个功能 ----
    // GameTimer 上需要的三样东西全是 public：
    //     GameTimer.Instance            (public static 字段)
    //     GameTimer.CurrentTime         (public Single 字段，单位秒)
    //     GameTimer.Reset()             (public 方法)
    // 而 Reset() 的实现只有两条指令：
    //     0000: ldarg.0
    //     0001: ldc.r4  0x00000000
    //     0006: stfld   GameTimer::CurrentTime
    //     000B: ret
    // 即"把 CurrentTime 写 0"本身。所以直接调官方方法即可，零反射、零偏移。
    //
    // ---- 为什么挂主线程钩子 ----
    // CurrentTime 是"值语义"的累加器，不是由其它状态派生的缓存，因此不存在被重算
    // 覆盖的问题。全程序集里写它的地方只有三处（都在 GameTimer 内部）：
    //     GameTimer::Reset           stfld（就是本功能调的）
    //     GameTimer::FixedUpdate     CurrentTime += Time.deltaTime
    //     GameTimer::Serialize       ldflda（读档时从存档还原）
    // 读取者有四类，且都不需要我们去驱动：
    //     GameController::get_GameTimeInSeconds    Mathf.RoundToInt(Timer.CurrentTime)
    //     TimeCounterDisplay::Update               GUIText（屏幕右上角计时）
    //     InventoryManager                         暂停界面的 H:MM:SS
    //     SaveSlotInfo::FillData                   Hours / Minutes / Seconds ← 写进存档槽
    // Reset() 自身只写一个 float 字段，没有任何 Unity native 调用。本来用定时器即可，
    // 但原版游戏（Unity 5.0 内置的旧 Mono 2.x）里注入 DLL 的 System.Threading.Timer
    // 不可靠（回调不触发，实测），所以统一改挂游戏自己的每帧回调 OnGameFixedUpdate。
    // TimeCounterDisplay 每 1 秒读一次显示串（m_delay 节流），所以归零后最多 1 秒
    // 界面就同步显示 0。
    //
    // ---- 为什么不做值判断（与 ZeroDeaths 的差别）----
    // ZeroDeaths 里加了 `if (Count != 0)` 是因为死亡数平时不变，判断能省掉绝大多数写入。
    // 这里恰好相反：游戏每个 FixedUpdate（50Hz，20ms）都 += deltaTime，而本钩子也是
    // 每个 FixedUpdate，所以 CurrentTime 几乎从不为 0 —— 判断恒真，只是白白多一次读取。
    // 直接调 Reset() 更省也更直白。
    //
    // ---- 停止不还原 ----
    // CurrentTime 是存档字段（GameTimer.Serialize 里就是 ar.Serialize(ref CurrentTime)），
    // 开启期间只要游戏存过一次档（过检查点 / 买技能 / 建灵魂链接），0 就已落盘，
    // 还原也收不回来。停止后计时从 0 继续正常累计。
    //
    // ---- 连带效果（会影响成就与排行榜）----
    //   · AchievementsLogic.<OnAct3EndIEnumerator>::MoveNext 通关时判定
    //     GameController.get_GameTimeInSeconds()，用于授予 FinishGameUnder6HoursAchievementAsset。
    //     归零后该判定必然通过。（该处常量是 10800 秒 = 3 小时，与成就名 Under6Hours
    //     不一致，此处只记录 IL 事实。）
    //   · LeaderboardsController.UploadScores 上传的 time 也取自
    //     get_GameTimeInSeconds，因此排行榜时间同样会变成 0。
    //   · 存档槽上的游玩时间也会变 0：SaveGameController.PerformSave 会调
    //     SaveSlotInfo.FillData()，而它从 GameController.Instance.Timer 取
    //     Hours / Minutes / Seconds 写进槽位信息。开启期间只要存过一次档，
    //     存档选择界面上该槽显示的时间就是 0，且随存档一起落盘。
    //   三者与本功能的用途一致，故不额外处理；但需知悉这是"通关计时、排行榜时间、
    //   存档槽显示真的都变 0"，而不只是屏幕上的那个计时器显示为 0。
    public static class ResetTime
    {
        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 CurrentTime：见文件头"停止不还原"
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                // 主菜单/读档过程中该单例可能尚未建立；GameTimer 是 MonoBehaviour，
                // 这里的 != null 走 UnityEngine.Object::op_Equality，已销毁对象的假空
                // （托管引用还在、原生对象没了）同样会被判为 null。
                // 每次都重新读静态字段：换场景/读档会重建 GameTimer。
                GameTimer timer = GameTimer.Instance;
                if (timer == null) return;

                timer.Reset(); // 官方方法，等价于 CurrentTime = 0f
            }
            catch { }
        }
    }
}
