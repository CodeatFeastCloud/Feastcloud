import { describe, expect, it } from 'vitest';
import { parseMenuCsv, previewRestaurantMenu, reviewPetpoojaMenu } from './menuImport';

const items = `Name,Online_Name,Description,Short_Code,Parent_Category,Category,Category_online_display,Price,Attributes,Rank,Addon_Group_Name,Addon_Group_Selection,Addon_Group_Min,Addon_Group_Max,Variation_group_name,Variation,Variation_Price,Addon_Group_Name,Addon_Group_Selection,Addon_Group_Min,Addon_Group_Max,Variation,Variation_Price\n"Paneer, Special",Paneer Special,"Slow cooked, rich",PANEER-1,Curries,Curries,House curries,0,veg,2,Add a bread,S,0,2,Portion,Serves 1,299,Add a bread,S,0,2,Serves 2,479\nChicken Bowl,Chicken Bowl,,CHICKEN-1,Bowls,Bowls,Quick bowls,249,non-veg,1,,,,,,,,,,,,`;
const addons = `Addon_Group_Id,Addon_Group_Name,Addon_Group_online_display,Addon_Group_status,Addon_Min,Addon_Max,Addon_Item_Selection,Max_Selection_Per_Addon,Show_In_Online,Show_In_Pos,Addon_Group_Show_In_Captain,Allow_Open_Quantity,Addon_Item_Name,Addon_Item_Price,Addon_item_status,Attribute,Rank,Sapcode,Show_In_Captain\n1,Add a bread,Add bread,Yes,0,2,S,1,Yes,Yes,Yes,No,Garlic Naan,79,Yes,veg,2,BREAD-1,Yes\n1,Add a bread,Add bread,Yes,0,2,S,1,Yes,Yes,Yes,No,Plain Naan,49,Yes,veg,1,BREAD-2,Yes`;

describe('Petpooja menu import review', () => {
  it('keeps quoted commas and duplicate add-on columns intact', () => {
    expect(parseMenuCsv(items).rows[0][0]).toBe('Paneer, Special');
  });

  it('creates a review with variants, category grouping and add-on links', () => {
    const review = reviewPetpoojaMenu(items, addons);
    expect(review.items).toHaveLength(2);
    expect(review.categories).toEqual([{ name: 'Bowls', itemCount: 1 }, { name: 'Curries', itemCount: 1 }]);
    expect(review.items[0]).toMatchObject({ onlineName: 'Paneer Special', priceMinor: 0, addonBindings: [{ name: 'Add a bread', selection: 'single', minimum: 0, maximum: 2 }], variations: [{ groupName: 'Portion', name: 'Serves 1', priceMinor: 29900 }, { groupName: 'Portion', name: 'Serves 2', priceMinor: 47900 }] });
    expect(review.addonGroups[0].options.map((option) => option.name)).toEqual(['Plain Naan', 'Garlic Naan']);
    expect(review.unresolvedAddonBindings).toBe(0);
  });

  it('marks links that are not in the add-on export for review', () => {
    const review = reviewPetpoojaMenu(items.replaceAll('Add a bread', 'Not exported'), addons);
    expect(review.unresolvedAddonBindings).toBe(1);
    expect(review.items[0].issues).toContain('Add-on group not found: Not exported');
  });

  it('finds the schema after the blank marker used by a live add-on export', () => {
    const review = reviewPetpoojaMenu(items, `,\n${addons}`);
    expect(review.addonGroups).toHaveLength(1);
    expect(review.addonGroups[0]).toMatchObject({ name: 'Add a bread', options: [{ name: 'Plain Naan' }, { name: 'Garlic Naan' }] });
  });

  it('preserves every add-on category mapped to one item', () => {
    const multiItems = `Name,Short_Code,Category,Price,Addon_Group_Name,Addon_Group_Selection,Addon_Group_Min,Addon_Group_Max,Addon_Group_Name,Addon_Group_Selection,Addon_Group_Min,Addon_Group_Max\nPaneer Roll,ROLL,Rolls,200,Add a bread,M,0,3,Choose a drink,S,1,1`;
    const multiAddons = `Addon_Group_Id,Addon_Group_Name,Addon_Group_online_display,Addon_Min,Addon_Max,Addon_Item_Selection,Show_In_Online,Show_In_Pos,Addon_Item_Name,Addon_Item_Price,Addon_item_status,Rank\n1,Add a bread,Breads,0,3,M,Yes,Yes,Garlic Naan,50,Yes,1\n2,Choose a drink,Drinks,1,1,S,Yes,Yes,Cola,40,Yes,1`;

    const preview = previewRestaurantMenu(multiItems, multiAddons);

    expect(preview.items[0].addOnGroupNames).toEqual(['Add a bread', 'Choose a drink']);
    expect(preview.addonGroups).toEqual(expect.arrayContaining([
      expect.objectContaining({ name: 'Add a bread', onlineName: 'Breads' }),
      expect.objectContaining({ name: 'Choose a drink', onlineName: 'Drinks' }),
    ]));
  });

  it('normalizes common item CSV headers through the same import preview', () => {
    const preview = previewRestaurantMenu('Item Name,SKU,Category,Selling Price,Item Description\nMasala dosa,DOSA-1,Breakfast,"₹120.50",Crisp rice crepe');

    expect(preview.items).toEqual([expect.objectContaining({
      name: 'Masala dosa', code: 'DOSA-1', category: 'Breakfast', priceMinor: 12050, description: 'Crisp rice crepe',
    })]);
  });
});
