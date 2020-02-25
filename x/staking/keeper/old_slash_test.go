package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// TODO integrate with test_common.go helper (CreateTestInput)
// setup helper function - creates two validators
func setupHelper(t *testing.T, power int64) (sdk.Context, Keeper, types.Params) {
	// setup
	ctx, _, _, keeper, _ := CreateTestInput(t, false, power)
	params := keeper.GetParams(ctx)
	numVals := int64(3)
	amt := sdk.TokensFromConsensusPower(power)
	bondedCoins := sdk.NewCoins(sdk.NewCoin(keeper.BondDenom(ctx), amt.MulRaw(numVals)))

	bondedPool := keeper.GetBondedPool(ctx)
	require.NoError(t, keeper.bankKeeper.SetBalances(ctx, bondedPool.GetAddress(), bondedCoins))
	keeper.supplyKeeper.SetModuleAccount(ctx, bondedPool)

	// add numVals validators
	for i := int64(0); i < numVals; i++ {
		validator := types.NewValidator(addrVals[i], PKs[i], types.Description{})
		validator, _ = validator.AddTokensFromDel(amt)
		validator = TestingUpdateValidator(keeper, ctx, validator, true)
		keeper.SetValidatorByConsAddr(ctx, validator)
	}

	return ctx, keeper, params
}

//_________________________________________________________________________________
// tests Slash at a previous height with a redelegation
func TestSlashWithRedelegation(t *testing.T) {
	ctx, keeper, _ := setupHelper(t, 10)
	consAddr := sdk.ConsAddress(PKs[0].Address())
	fraction := sdk.NewDecWithPrec(5, 1)
	bondDenom := keeper.BondDenom(ctx)

	// set a redelegation
	rdTokens := sdk.TokensFromConsensusPower(6)
	rd := types.NewRedelegation(addrDels[0], addrVals[0], addrVals[1], 11,
		time.Unix(0, 0), rdTokens, rdTokens.ToDec())
	keeper.SetRedelegation(ctx, rd)

	// set the associated delegation
	del := types.NewDelegation(addrDels[0], addrVals[1], rdTokens.ToDec())
	keeper.SetDelegation(ctx, del)

	// update bonded tokens
	bondedPool := keeper.GetBondedPool(ctx)
	notBondedPool := keeper.GetNotBondedPool(ctx)
	rdCoins := sdk.NewCoins(sdk.NewCoin(bondDenom, rdTokens.MulRaw(2)))

	balances := keeper.bankKeeper.GetAllBalances(ctx, bondedPool.GetAddress())
	err := keeper.bankKeeper.SetBalances(ctx, bondedPool.GetAddress(), balances.Add(rdCoins...))
	require.NoError(t, err)

	keeper.supplyKeeper.SetModuleAccount(ctx, bondedPool)

	oldBonded := keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	oldNotBonded := keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount

	// slash validator
	ctx = ctx.WithBlockHeight(12)
	validator, found := keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.True(t, found)

	require.NotPanics(t, func() { keeper.Slash(ctx, consAddr, 10, 10, fraction) })
	burnAmount := sdk.TokensFromConsensusPower(10).ToDec().Mul(fraction).TruncateInt()

	bondedPool = keeper.GetBondedPool(ctx)
	notBondedPool = keeper.GetNotBondedPool(ctx)

	// burn bonded tokens from only from delegations
	bondedPoolBalance := keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldBonded.Sub(burnAmount), bondedPoolBalance))

	notBondedPoolBalance := keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldNotBonded, notBondedPoolBalance))
	oldBonded = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount

	// read updating redelegation
	rd, found = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.True(t, found)
	require.Len(t, rd.Entries, 1)
	// read updated validator
	validator, found = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.True(t, found)
	// power decreased by 2 - 4 stake originally bonded at the time of infraction
	// was still bonded at the time of discovery and was slashed by half, 4 stake
	// bonded at the time of discovery hadn't been bonded at the time of infraction
	// and wasn't slashed
	require.Equal(t, int64(8), validator.GetConsensusPower())

	// slash the validator again
	validator, found = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.True(t, found)

	require.NotPanics(t, func() { keeper.Slash(ctx, consAddr, 10, 10, sdk.OneDec()) })
	burnAmount = sdk.TokensFromConsensusPower(7)

	// read updated pool
	bondedPool = keeper.GetBondedPool(ctx)
	notBondedPool = keeper.GetNotBondedPool(ctx)

	// seven bonded tokens burned
	bondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldBonded.Sub(burnAmount), bondedPoolBalance))
	require.True(sdk.IntEq(t, oldNotBonded, notBondedPoolBalance))

	bondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldBonded.Sub(burnAmount), bondedPoolBalance))

	notBondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldNotBonded, notBondedPoolBalance))
	oldBonded = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount

	// read updating redelegation
	rd, found = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.True(t, found)
	require.Len(t, rd.Entries, 1)
	// read updated validator
	validator, found = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.True(t, found)
	// power decreased by 4
	require.Equal(t, int64(4), validator.GetConsensusPower())

	// slash the validator again, by 100%
	ctx = ctx.WithBlockHeight(12)
	validator, found = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.True(t, found)

	require.NotPanics(t, func() { keeper.Slash(ctx, consAddr, 10, 10, sdk.OneDec()) })

	burnAmount = sdk.TokensFromConsensusPower(10).ToDec().Mul(sdk.OneDec()).TruncateInt()
	burnAmount = burnAmount.Sub(sdk.OneDec().MulInt(rdTokens).TruncateInt())

	// read updated pool
	bondedPool = keeper.GetBondedPool(ctx)
	notBondedPool = keeper.GetNotBondedPool(ctx)

	bondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldBonded.Sub(burnAmount), bondedPoolBalance))
	notBondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldNotBonded, notBondedPoolBalance))
	oldBonded = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount

	// read updating redelegation
	rd, found = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.True(t, found)
	require.Len(t, rd.Entries, 1)
	// apply TM updates
	keeper.ApplyAndReturnValidatorSetUpdates(ctx)
	// read updated validator
	// validator decreased to zero power, should be in unbonding period
	validator, _ = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.Equal(t, validator.GetStatus(), sdk.Unbonding)

	// slash the validator again, by 100%
	// no stake remains to be slashed
	ctx = ctx.WithBlockHeight(12)
	// validator still in unbonding period
	validator, _ = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.Equal(t, validator.GetStatus(), sdk.Unbonding)

	require.NotPanics(t, func() { keeper.Slash(ctx, consAddr, 10, 10, sdk.OneDec()) })

	// read updated pool
	bondedPool = keeper.GetBondedPool(ctx)
	notBondedPool = keeper.GetNotBondedPool(ctx)

	bondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldBonded, bondedPoolBalance))
	notBondedPoolBalance = keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldNotBonded, notBondedPoolBalance))

	// read updating redelegation
	rd, found = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.True(t, found)
	require.Len(t, rd.Entries, 1)
	// read updated validator
	// power still zero, still in unbonding period
	validator, _ = keeper.GetValidatorByConsAddr(ctx, consAddr)
	require.Equal(t, validator.GetStatus(), sdk.Unbonding)
}

// tests Slash at a previous height with both an unbonding delegation and a redelegation
func TestSlashBoth(t *testing.T) {
	ctx, keeper, _ := setupHelper(t, 10)
	fraction := sdk.NewDecWithPrec(5, 1)
	bondDenom := keeper.BondDenom(ctx)

	// set a redelegation with expiration timestamp beyond which the
	// redelegation shouldn't be slashed
	rdATokens := sdk.TokensFromConsensusPower(6)
	rdA := types.NewRedelegation(addrDels[0], addrVals[0], addrVals[1], 11,
		time.Unix(0, 0), rdATokens,
		rdATokens.ToDec())
	keeper.SetRedelegation(ctx, rdA)

	// set the associated delegation
	delA := types.NewDelegation(addrDels[0], addrVals[1], rdATokens.ToDec())
	keeper.SetDelegation(ctx, delA)

	// set an unbonding delegation with expiration timestamp (beyond which the
	// unbonding delegation shouldn't be slashed)
	ubdATokens := sdk.TokensFromConsensusPower(4)
	ubdA := types.NewUnbondingDelegation(addrDels[0], addrVals[0], 11,
		time.Unix(0, 0), ubdATokens)
	keeper.SetUnbondingDelegation(ctx, ubdA)

	bondedCoins := sdk.NewCoins(sdk.NewCoin(bondDenom, rdATokens.MulRaw(2)))
	notBondedCoins := sdk.NewCoins(sdk.NewCoin(bondDenom, ubdATokens))

	// update bonded tokens
	bondedPool := keeper.GetBondedPool(ctx)
	notBondedPool := keeper.GetNotBondedPool(ctx)

	bondedPoolBalances := keeper.bankKeeper.GetAllBalances(ctx, bondedPool.GetAddress())
	require.NoError(t, keeper.bankKeeper.SetBalances(ctx, bondedPool.GetAddress(), bondedPoolBalances.Add(bondedCoins...)))

	notBondedPoolBalances := keeper.bankKeeper.GetAllBalances(ctx, notBondedPool.GetAddress())
	require.NoError(t, keeper.bankKeeper.SetBalances(ctx, notBondedPool.GetAddress(), notBondedPoolBalances.Add(notBondedCoins...)))

	keeper.supplyKeeper.SetModuleAccount(ctx, bondedPool)
	keeper.supplyKeeper.SetModuleAccount(ctx, notBondedPool)

	oldBonded := keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	oldNotBonded := keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount
	// slash validator
	ctx = ctx.WithBlockHeight(12)
	validator, found := keeper.GetValidatorByConsAddr(ctx, sdk.GetConsAddress(PKs[0]))
	require.True(t, found)
	consAddr0 := sdk.ConsAddress(PKs[0].Address())
	keeper.Slash(ctx, consAddr0, 10, 10, fraction)

	burnedNotBondedAmount := fraction.MulInt(ubdATokens).TruncateInt()
	burnedBondAmount := sdk.TokensFromConsensusPower(10).ToDec().Mul(fraction).TruncateInt()
	burnedBondAmount = burnedBondAmount.Sub(burnedNotBondedAmount)

	// read updated pool
	bondedPool = keeper.GetBondedPool(ctx)
	notBondedPool = keeper.GetNotBondedPool(ctx)

	bondedPoolBalance := keeper.bankKeeper.GetBalance(ctx, bondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldBonded.Sub(burnedBondAmount), bondedPoolBalance))

	notBondedPoolBalance := keeper.bankKeeper.GetBalance(ctx, notBondedPool.GetAddress(), bondDenom).Amount
	require.True(sdk.IntEq(t, oldNotBonded.Sub(burnedNotBondedAmount), notBondedPoolBalance))

	// read updating redelegation
	rdA, found = keeper.GetRedelegation(ctx, addrDels[0], addrVals[0], addrVals[1])
	require.True(t, found)
	require.Len(t, rdA.Entries, 1)
	// read updated validator
	validator, found = keeper.GetValidatorByConsAddr(ctx, sdk.GetConsAddress(PKs[0]))
	require.True(t, found)
	// power not decreased, all stake was bonded since
	require.Equal(t, int64(10), validator.GetConsensusPower())
}
