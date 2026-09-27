using System;

namespace OriTrainerDLL.Features
{
    // 解锁全部基础技能：把暂停界面里显示的 11 项基础能力全部授予。
    //
    // 只给基础能力，**不给灵魂链接技能树里的被动**（UltraDefense / RapidFire / 各种
    // *Efficiency 等 32 项）—— 那些让玩家用"无限能力点数"自己去买。
    // 游戏把两者建模成同一个类型 CharacterAbility（只有一个 HasAbility 布尔），
    // 无法靠类型区分，只能按字段清单区分。
    //
    // ---- 为什么必须挂主线程钩子 ----
    // 只写 HasAbility 是**没用的**：能力组件要经
    //     SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities()
    // 才会实体化，而它内部走 Object.Instantiate 创建 GameObject —— Unity API，
    // 必须在主线程调用（参见此前从定时器线程调 Application.Quit() 崩游戏的教训）。
    // 因此与 InfiniteDash / InfiniteDoubleJump 一样挂到
    //     Game.Events.Scheduler.OnGameFixedUpdate
    //
    // ---- 为什么持续写入，而不是写一次就完 ----
    // 唯一会把这些标志位"打回"的地方是 SceneMetaData+SeinInitialValues.ApplyInitialValues，
    // 它按场景元数据无条件写回 38 项能力。它只在 GameController.SetupGameplay 里被调用，
    // 而后者被一道一次性闸门挡着（SeinPlaceholder.AfterLoadingFromMasterFinishedAfterInstantiation
    //   里 RequireInitialValues 为真才调 SetupGameplay，且调用前先把它置回 false）。
    // 于是问题归结为"谁会重新打开这道闸门"（全程序集共 10 处置位）：
    //   · 换场景【不会】—— LoadSceneAction 只在 UseSceneInitialValues 为真时才置位；
    //   · 普通死亡恢复检查点【不会】—— 那条路径完全不碰这道闸门。
    // 但下列操作会：RestartGame / RestartOneLifeMode / NewGameAction /
    // SkipCutsceneController.SkipPrologue / GoToSequenceMenuItem / WatchCutsceneAction /
    // SetGameModeToPrologueAction / ResetStateForDebugMenuGoToScene，
    // 也就是"回标题再读档""重开一局""跳过序章"这类操作仍会打回。
    //
    // 本功能持续压制，以上情况都会自动补回；另外每帧调用 EnsureRightPrefabs 对同值
    // 会提前 return（set_IsInstantiated 有同值守卫），开销可忽略。
    //
    // ---- 为什么不用游戏自己的 PlayerAbilities.SetAbility ----
    // 它是 public，且尾部就带 EnsureRightPrefabs，看起来正合适。但传递闭包分析显示
    // 它 4 层内可达 13 个 Unity native 调用（Object.Instantiate / GameObject.AddComponent /
    // Transform.set_position 等），且 SpiritFlame 分支额外调 Ori.MoveOriToPlayer()
    // → m_transform.position = m_target.position + TargetOffset，**会把黑子瞬移到玩家身边**。
    // 直接写字段没有这些副作用，而且字段全是 public，连反射都不需要。
    // （SetAllAbilitys(bool) 也不用：它会把技能树那 32 项被动一并给全，超出本功能语义。）
    //
    // ---- 停止不还原 ----
    // PlayerAbilities.Serialize() 会遍历全部 43 项把 HasAbility 写进存档，所以开启后
    // 只要游戏存过一次档（过检查点 / 买技能 / 建灵魂链接），这些能力就成了存档里的
    // 既有能力，还原也收不回来（与 InfiniteDash 的 AirDash 同理）。
    // 刻意不还原也避免 SeinNestedPrefab.IsInstantiated 置 false 时 Destroy() 掉组件，
    // 导致"关了再开就失效"。
    //
    // ⚠ 11 项里只有 9 项由 EnsureRightPrefabsAreThereForAbilities 实例化：
    //     WallJump / Stomp / DoubleJump / ChargeJump / Climb / Bash / Glide / Dash / Grenade
    //   （按该函数 IL 里 set_IsInstantiated 的调用顺序。它顺带还会置 WallSlide，
    //    但那是 WallJump 的附带项，不单独对应哪一项能力。）
    //   不在其中的是 SpiritFlame 和 ChargeFlame，原因【各不相同】：
    //     · SpiritFlame —— SeinPrefabFactory 有它的 nested prefab，但
    //       SeinPrefabFactory.Awake() 已经自动实例化了（实测 Awake 里置了 13 个），
    //       所以不需要这个函数再管。
    //     · ChargeFlame —— SeinPrefabFactory 里根本没有它的 nested prefab 字段，
    //       组件由别处创建，这个函数管不到。
    //   另注：Climb 对应的 prefab 字段名叫 GrabWall（"抓住墙"），不是 Climb，
    //   按名字找会以为它没被覆盖。
    public static class UnlockAllAbilities
    {
        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // Scheduler 由 GameController 持有，而 GameController.Awake 是单例守卫
            // （Instance 已存在则 Destroy 自身），所以该回调在整个进程内稳定可用。
            // 游戏尚未启动到 GameController 时取出会得到 null，直接报错更易排查。
            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 HasAbility：见文件头"停止不还原"
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                // 主菜单/读档过程中 Sein 为 null，显式判空
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                // 11 项基础能力，全是 public 的 CharacterAbility 字段，直接赋值。
                // 名单与 golang 版 BaseAbilityOffsets 一致（终极版 11 项；原版没有
                // Grenade / Dash，本修改器只针对终极版，不做版本分支）。
                playerAbilities.Bash.HasAbility = true;         // 猛击
                playerAbilities.ChargeFlame.HasAbility = true;  // 充能烈焰
                playerAbilities.WallJump.HasAbility = true;     // 飞檐走壁
                playerAbilities.Stomp.HasAbility = true;        // 践踏攻击
                playerAbilities.DoubleJump.HasAbility = true;   // 二段跳
                playerAbilities.ChargeJump.HasAbility = true;   // 充能跳跃
                playerAbilities.Climb.HasAbility = true;        // 攀爬
                playerAbilities.Glide.HasAbility = true;        // 黑子之羽
                playerAbilities.SpiritFlame.HasAbility = true;  // 精灵之火
                playerAbilities.Grenade.HasAbility = true;      // 光芒爆裂（终极版）
                playerAbilities.Dash.HasAbility = true;         // 冲刺（终极版）

                // 实例化对应的能力组件（本功能唯一需要主线程的一步，也是 golang 版缺的一步）。
                // set_IsInstantiated 对同值会提前 return，每帧调用没有开销。
                SeinPrefabFactory prefabs = sein.Prefabs;
                prefabs?.EnsureRightPrefabsAreThereForAbilities();
            }
            catch { }
        }
    }
}
