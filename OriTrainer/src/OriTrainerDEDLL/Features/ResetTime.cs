using System;
using System.Threading;

namespace OriTrainerDEDLL.Features
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
    // ---- 为什么不需要主线程钩子 ----
    // CurrentTime 是"值语义"的累加器，不是由其它状态派生的缓存，因此不存在被重算
    // 覆盖的问题。全程序集里写它的地方只有三处：
    //     GameTimer::Reset           stfld（就是本功能调的）
    //     GameTimer::FixedUpdate     CurrentTime += Time.deltaTime
    //     GameTimer::Serialize       ldflda（读档时从存档还原）
    // 外部读取者也只有两处，且都不需要我们去驱动：
    //     GameController::get_GameTimeInSeconds   Mathf.RoundToInt(Timer.CurrentTime)
    //     TimeCounterDisplay::Update              GUIText.set_text(get_DisplayTimeAsString())
    // Reset() 自身只写一个 float 字段，没有任何 Unity native 调用，所以与
    // UnlimitedLife / ZeroDeaths 一样用定时器即可，不像 InfiniteDash /
    // InfiniteDoubleJump / ShowMap / UnlockAllAbilities 那样必须挂 OnGameFixedUpdate。
    // TimeCounterDisplay 每 1 秒读一次显示串（m_delay 节流），所以归零后最多 1 秒
    // 界面就同步显示 0。
    //
    // ---- 为什么不做值判断（与 ZeroDeaths 的差别）----
    // ZeroDeaths 里加了 `if (Count != 0)` 是因为死亡数平时不变，判断能省掉绝大多数写入。
    // 这里恰好相反：游戏每个 FixedUpdate（50Hz，20ms）都 += deltaTime，而本定时器是
    // 10ms，所以 CurrentTime 几乎从不为 0 —— 判断恒真，只是白白多一次读取。
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
    //   两者与本功能的用途一致，故不额外处理；但需知悉这是"通关计时真的变 0"，
    //   而不只是界面显示为 0。
    public static class ResetTime
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

            // 刻意不还原 CurrentTime：见文件头"停止不还原"
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
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
